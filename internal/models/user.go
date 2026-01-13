/**
 * internal/models/user.go
 * 用户模型和数据访问层
 *
 * 功能：
 * - 用户数据结构定义
 * - 用户 CRUD 操作
 * - 用户查询（按 ID、邮箱、用户名、Microsoft ID）
 * - 数据验证和错误处理
 *
 * 依赖：
 * - PostgreSQL 数据库连接池
 */

package models

import (
	"auth-system/internal/utils"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ====================  错误定义 ====================

var (
	// ErrUserNotFound 用户未找到
	ErrUserNotFound = errors.New("USER_NOT_FOUND")
	// ErrEmailExists 邮箱已存在
	ErrEmailExists = errors.New("EMAIL_EXISTS")
	// ErrUsernameExists 用户名已存在
	ErrUsernameExists = errors.New("USERNAME_EXISTS")
	// ErrMicrosoftIDExists Microsoft ID 已存在
	ErrMicrosoftIDExists = errors.New("MICROSOFT_ID_EXISTS")
	// ErrInvalidUserData 无效的用户数据
	ErrInvalidUserData = errors.New("INVALID_USER_DATA")
	// ErrUserRepoDBNotReady 数据库未就绪
	ErrUserRepoDBNotReady = errors.New("database not ready")
	// ErrUserRepoNilUser 用户对象为空
	ErrUserRepoNilUser = errors.New("user object is nil")
	// ErrUserRepoInvalidID 无效的用户 ID
	ErrUserRepoInvalidID = errors.New("invalid user ID")
	// ErrUserRepoEmptyIdentifier 空的查询标识符
	ErrUserRepoEmptyIdentifier = errors.New("empty identifier")
)

// ====================  常量定义 ====================

const (
	// DefaultAvatar 默认头像 URL
	DefaultAvatar = "https://cdn01.nebulastudios.top/images/default-avatar.svg"

	// maxUpdateFields 最大更新字段数
	maxUpdateFields = 10

	// RoleUser 普通用户（无后台权限）
	RoleUser = 0
	// RoleAdmin 普通管理员（日常管理权限）
	RoleAdmin = 1
	// RoleSuperAdmin 超级管理员（全部权限）
	RoleSuperAdmin = 2
)

// allowedUpdateFields 允许更新的字段白名单
var allowedUpdateFields = map[string]bool{
	"username":              true,
	"email":                 true,
	"password":              true,
	"avatar_url":            true,
	"microsoft_id":          true,
	"microsoft_name":        true,
	"microsoft_avatar_url":  true,
	"microsoft_avatar_hash": true,
	"role":                  true,
}

// ====================  数据结构 ====================

// User 用户模型
type User struct {
	ID                  int64          `json:"id"`
	Username            string         `json:"username"`
	Email               string         `json:"email"`
	Password            string         `json:"-"` // 不序列化到 JSON
	AvatarURL           string         `json:"avatar_url"`
	Role                int            `json:"role"` // 0: user, 1: admin, 2: super_admin
	MicrosoftID         sql.NullString `json:"microsoft_id,omitempty"`
	MicrosoftName       sql.NullString `json:"microsoft_name,omitempty"`
	MicrosoftAvatarURL  sql.NullString `json:"microsoft_avatar_url,omitempty"`
	MicrosoftAvatarHash sql.NullString `json:"-"`              // 头像哈希，用于判断是否需要更新
	IsBanned            bool           `json:"is_banned"`      // 是否被封禁
	BanReason           sql.NullString `json:"ban_reason"`     // 封禁原因
	BannedAt            sql.NullTime   `json:"banned_at"`      // 封禁时间
	BannedBy            sql.NullInt64  `json:"banned_by"`      // 封禁操作者 ID
	UnbanAt             sql.NullTime   `json:"unban_at"`       // 解封时间（NULL 表示永封）
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

// UserPublic 公开的用户信息（不含敏感数据）
type UserPublic struct {
	ID                 int64      `json:"id"`
	Username           string     `json:"username"`
	Email              string     `json:"email"`
	AvatarURL          string     `json:"avatar_url"`
	Role               int        `json:"role"`
	MicrosoftID        *string    `json:"microsoft_id,omitempty"`
	MicrosoftName      *string    `json:"microsoft_name,omitempty"`
	MicrosoftAvatarURL *string    `json:"microsoft_avatar_url,omitempty"`
	IsBanned           bool       `json:"is_banned"`
	BanReason          *string    `json:"ban_reason,omitempty"`
	BannedAt           *time.Time `json:"banned_at,omitempty"`
	UnbanAt            *time.Time `json:"unban_at,omitempty"` // NULL 表示永封
	CreatedAt          time.Time  `json:"created_at"`
}

// UserRepository 用户仓库
type UserRepository struct{}

// ====================  User 方法 ====================

// ToPublic 转换为公开信息
// 返回：
//   - *UserPublic: 公开的用户信息
func (u *User) ToPublic() *UserPublic {
	if u == nil {
		return nil
	}

	// 处理头像 URL：如果是 "microsoft" 标记，使用微软头像
	avatarURL := u.AvatarURL
	if avatarURL == "microsoft" && u.MicrosoftAvatarURL.Valid {
		avatarURL = u.MicrosoftAvatarURL.String
	}

	pub := &UserPublic{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		AvatarURL: avatarURL,
		Role:      u.Role,
		IsBanned:  u.IsBanned,
		CreatedAt: u.CreatedAt,
	}

	if u.MicrosoftID.Valid {
		pub.MicrosoftID = &u.MicrosoftID.String
	}
	if u.MicrosoftName.Valid {
		pub.MicrosoftName = &u.MicrosoftName.String
	}
	if u.MicrosoftAvatarURL.Valid {
		pub.MicrosoftAvatarURL = &u.MicrosoftAvatarURL.String
	}
	if u.BanReason.Valid {
		pub.BanReason = &u.BanReason.String
	}
	if u.BannedAt.Valid {
		pub.BannedAt = &u.BannedAt.Time
	}
	if u.UnbanAt.Valid {
		pub.UnbanAt = &u.UnbanAt.Time
	}

	return pub
}

// IsAdmin 检查是否为管理员（包括超级管理员）
// 返回：
//   - bool: 是否为管理员
func (u *User) IsAdmin() bool {
	return u != nil && u.Role >= RoleAdmin
}

// IsSuperAdmin 检查是否为超级管理员
// 返回：
//   - bool: 是否为超级管理员
func (u *User) IsSuperAdmin() bool {
	return u != nil && u.Role >= RoleSuperAdmin
}

// CheckBanned 检查用户是否处于封禁状态
// 会自动检查解封时间，如果已过期则返回 false
// 返回：
//   - bool: 是否被封禁
func (u *User) CheckBanned() bool {
	if u == nil || !u.IsBanned {
		return false
	}
	// 如果有解封时间且已过期，则不再封禁
	if u.UnbanAt.Valid && time.Now().After(u.UnbanAt.Time) {
		return false
	}
	return true
}

// IsPermanentBan 检查是否为永久封禁
// 返回：
//   - bool: 是否为永久封禁
func (u *User) IsPermanentBan() bool {
	return u != nil && u.IsBanned && !u.UnbanAt.Valid
}

// Validate 验证用户数据
// 返回：
//   - error: 验证失败时返回错误
func (u *User) Validate() error {
	if u == nil {
		return ErrUserRepoNilUser
	}
	if u.Username == "" {
		return fmt.Errorf("%w: username is empty", ErrInvalidUserData)
	}
	if u.Email == "" {
		return fmt.Errorf("%w: email is empty", ErrInvalidUserData)
	}
	if u.Password == "" {
		return fmt.Errorf("%w: password is empty", ErrInvalidUserData)
	}
	return nil
}

// ====================  构造函数 ====================

// NewUserRepository 创建用户仓库
// 返回：
//   - *UserRepository: 用户仓库实例
func NewUserRepository() *UserRepository {
	return &UserRepository{}
}

// ====================  查询方法 ====================

// FindByID 根据 ID 查找用户
// 参数：
//   - ctx: 上下文
//   - id: 用户 ID
//
// 返回：
//   - *User: 用户对象
//   - error: 错误信息
func (r *UserRepository) FindByID(ctx context.Context, id int64) (*User, error) {
	// 参数验证
	if id <= 0 {
		return nil, ErrUserRepoInvalidID
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, err
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE id = $1
	`, id).Scan(
		&user.ID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, r.handleQueryError(err, "FindByID", id)
	}

	return user, nil
}

// FindByEmailOrUsername 根据邮箱或用户名查找用户（登录优化）
// 单次查询同时匹配邮箱和用户名，提高登录性能
//
// 参数：
//   - ctx: 上下文
//   - identifier: 邮箱或用户名
//
// 返回：
//   - *User: 用户对象
//   - error: 错误信息
func (r *UserRepository) FindByEmailOrUsername(ctx context.Context, identifier string) (*User, error) {
	// 参数验证
	if identifier == "" {
		return nil, ErrUserRepoEmptyIdentifier
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, err
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE email = $1 OR username = $1
		LIMIT 1
	`, identifier).Scan(
		&user.ID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, r.handleQueryError(err, "FindByEmailOrUsername", identifier)
	}

	return user, nil
}

// FindByEmail 根据邮箱查找用户
// 参数：
//   - ctx: 上下文
//   - email: 邮箱地址
//
// 返回：
//   - *User: 用户对象
//   - error: 错误信息
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	// 参数验证
	if email == "" {
		return nil, ErrUserRepoEmptyIdentifier
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, err
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE email = $1
	`, email).Scan(
		&user.ID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, r.handleQueryError(err, "FindByEmail", email)
	}

	return user, nil
}

// FindByUsername 根据用户名查找用户
// 参数：
//   - ctx: 上下文
//   - username: 用户名
//
// 返回：
//   - *User: 用户对象
//   - error: 错误信息
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*User, error) {
	// 参数验证
	if username == "" {
		return nil, ErrUserRepoEmptyIdentifier
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, err
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE username = $1
	`, username).Scan(
		&user.ID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, r.handleQueryError(err, "FindByUsername", username)
	}

	return user, nil
}

// FindByMicrosoftID 根据 Microsoft ID 查找用户
// 参数：
//   - ctx: 上下文
//   - msID: Microsoft ID
//
// 返回：
//   - *User: 用户对象
//   - error: 错误信息
func (r *UserRepository) FindByMicrosoftID(ctx context.Context, msID string) (*User, error) {
	// 参数验证
	if msID == "" {
		return nil, ErrUserRepoEmptyIdentifier
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, err
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE microsoft_id = $1
	`, msID).Scan(
		&user.ID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, r.handleQueryError(err, "FindByMicrosoftID", msID)
	}

	return user, nil
}

// ====================  写入方法 ====================

// Create 创建用户
// 参数：
//   - ctx: 上下文
//   - user: 用户对象（ID、CreatedAt、UpdatedAt 会被自动填充）
//
// 返回：
//   - error: 错误信息
func (r *UserRepository) Create(ctx context.Context, user *User) error {
	// 参数验证
	if user == nil {
		return ErrUserRepoNilUser
	}

	// 验证用户数据
	if err := user.Validate(); err != nil {
		return err
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return err
	}

	// 设置默认头像
	if user.AvatarURL == "" {
		user.AvatarURL = DefaultAvatar
	}

	// 设置默认角色（普通用户）
	if user.Role == 0 {
		user.Role = RoleUser
	}

	// 执行插入
	err := pool.QueryRow(ctx, `
		INSERT INTO users (username, email, password, avatar_url, microsoft_id, role)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, user.Username, user.Email, user.Password, user.AvatarURL, user.MicrosoftID, user.Role).Scan(
		&user.ID, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return r.handleWriteError(err, "Create", user.Email)
	}

	utils.LogPrintf("[USER] User created: id=%d, email=%s", user.ID, user.Email)
	return nil
}

// Update 更新用户
// 参数：
//   - ctx: 上下文
//   - id: 用户 ID
//   - updates: 要更新的字段映射
//
// 返回：
//   - error: 错误信息
func (r *UserRepository) Update(ctx context.Context, id int64, updates map[string]interface{}) error {
	// 参数验证
	if id <= 0 {
		return ErrUserRepoInvalidID
	}

	if len(updates) == 0 {
		utils.LogPrintf("[USER] WARN: Update called with empty updates: id=%d", id)
		return nil
	}

	if len(updates) > maxUpdateFields {
		return fmt.Errorf("%w: too many update fields", ErrInvalidUserData)
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return err
	}

	// 构建动态 SQL（使用白名单验证字段）
	query, args, err := r.buildUpdateQuery(id, updates)
	if err != nil {
		return err
	}

	// 执行更新
	result, err := pool.Exec(ctx, query, args...)
	if err != nil {
		return r.handleWriteError(err, "Update", id)
	}

	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	utils.LogPrintf("[USER] User updated: id=%d, fields=%d", id, len(updates))
	return nil
}

// Delete 删除用户
// 参数：
//   - ctx: 上下文
//   - id: 用户 ID
//
// 返回：
//   - error: 错误信息
func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	// 参数验证
	if id <= 0 {
		return ErrUserRepoInvalidID
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return err
	}

	result, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", id)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to delete user: id=%d, error=%v", id, err)
		return fmt.Errorf("delete user failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	utils.LogPrintf("[USER] User deleted: id=%d", id)
	return nil
}

// ====================  私有方法 ====================

// checkDB 检查数据库连接是否就绪
func (r *UserRepository) checkDB() error {
	if pool == nil {
		utils.LogPrintf("[USER] ERROR: Database pool is nil")
		return ErrUserRepoDBNotReady
	}
	return nil
}

// handleQueryError 处理查询错误
// 参数：
//   - err: 原始错误
//   - operation: 操作名称
//   - identifier: 查询标识符
//
// 返回：
//   - error: 处理后的错误
func (r *UserRepository) handleQueryError(err error, operation string, identifier interface{}) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}

	// pgx 使用不同的错误类型，检查错误消息
	if err.Error() == "no rows in result set" {
		return ErrUserNotFound
	}

	utils.LogPrintf("[USER] ERROR: %s failed: identifier=%v, error=%v", operation, identifier, err)
	return fmt.Errorf("%s failed: %w", operation, err)
}

// handleWriteError 处理写入错误
// 参数：
//   - err: 原始错误
//   - operation: 操作名称
//   - identifier: 相关标识符
//
// 返回：
//   - error: 处理后的错误
func (r *UserRepository) handleWriteError(err error, operation string, identifier interface{}) error {
	errStr := err.Error()

	// 检查唯一约束冲突
	if strings.Contains(errStr, "users_email_key") {
		return ErrEmailExists
	}
	if strings.Contains(errStr, "users_username_key") {
		return ErrUsernameExists
	}
	if strings.Contains(errStr, "users_microsoft_id_key") {
		return ErrMicrosoftIDExists
	}

	utils.LogPrintf("[USER] ERROR: %s failed: identifier=%v, error=%v", operation, identifier, err)
	return fmt.Errorf("%s failed: %w", operation, err)
}

// buildUpdateQuery 构建更新 SQL 查询
// 参数：
//   - id: 用户 ID
//   - updates: 要更新的字段映射
//
// 返回：
//   - string: SQL 查询
//   - []interface{}: 参数列表
//   - error: 错误信息
func (r *UserRepository) buildUpdateQuery(id int64, updates map[string]interface{}) (string, []interface{}, error) {
	var setClauses []string
	args := make([]interface{}, 0, len(updates)+1)
	argIndex := 1

	for key, value := range updates {
		// 验证字段是否在白名单中（防止 SQL 注入）
		if !allowedUpdateFields[key] {
			utils.LogPrintf("[USER] WARN: Attempted to update disallowed field: %s", key)
			continue
		}

		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", key, argIndex))
		args = append(args, value)
		argIndex++
	}

	if len(setClauses) == 0 {
		return "", nil, fmt.Errorf("%w: no valid fields to update", ErrInvalidUserData)
	}

	// 添加 updated_at
	query := fmt.Sprintf(
		"UPDATE users SET updated_at = CURRENT_TIMESTAMP, %s WHERE id = $%d",
		strings.Join(setClauses, ", "),
		argIndex,
	)
	args = append(args, id)

	return query, args, nil
}

// ====================  管理后台方法 ====================

// UserStats 用户统计数据
type UserStats struct {
	TotalUsers      int64 `json:"totalUsers"`
	TodayNewUsers   int64 `json:"todayNewUsers"`
	AdminCount      int64 `json:"adminCount"`
	MicrosoftLinked int64 `json:"microsoftLinked"`
	BannedCount     int64 `json:"bannedCount"`
}

// FindAll 查询用户列表（分页、搜索）
// 参数：
//   - ctx: 上下文
//   - page: 页码（从 1 开始）
//   - pageSize: 每页数量
//   - search: 搜索关键词（匹配用户名或邮箱）
//
// 返回：
//   - []*User: 用户列表
//   - int64: 总数
//   - error: 错误信息
func (r *UserRepository) FindAll(ctx context.Context, page, pageSize int, search string) ([]*User, int64, error) {
	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, 0, err
	}

	// 计算偏移量
	offset := (page - 1) * pageSize

	var total int64
	var rows interface{ Close() }
	var err error

	if search == "" {
		// 无搜索条件
		err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&total)
		if err != nil {
			utils.LogPrintf("[USER] ERROR: Failed to count users: error=%v", err)
			return nil, 0, fmt.Errorf("count users failed: %w", err)
		}

		rows, err = pool.Query(ctx, `
			SELECT id, username, email, password, avatar_url, role,
			       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
			       is_banned, ban_reason, banned_at, banned_by, unban_at,
			       created_at, updated_at
			FROM users
			ORDER BY id DESC
			LIMIT $1 OFFSET $2
		`, pageSize, offset)
	} else {
		// 有搜索条件
		searchPattern := "%" + search + "%"
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM users 
			WHERE username ILIKE $1 OR email ILIKE $1
		`, searchPattern).Scan(&total)
		if err != nil {
			utils.LogPrintf("[USER] ERROR: Failed to count users with search: error=%v", err)
			return nil, 0, fmt.Errorf("count users failed: %w", err)
		}

		rows, err = pool.Query(ctx, `
			SELECT id, username, email, password, avatar_url, role,
			       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
			       is_banned, ban_reason, banned_at, banned_by, unban_at,
			       created_at, updated_at
			FROM users
			WHERE username ILIKE $1 OR email ILIKE $1
			ORDER BY id DESC
			LIMIT $2 OFFSET $3
		`, searchPattern, pageSize, offset)
	}

	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to query users: error=%v", err)
		return nil, 0, fmt.Errorf("query users failed: %w", err)
	}
	defer rows.Close()

	// 扫描结果
	users := make([]*User, 0)
	pgxRows := rows.(interface {
		Next() bool
		Scan(dest ...interface{}) error
	})

	for pgxRows.Next() {
		user := &User{}
		err := pgxRows.Scan(
			&user.ID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
			&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
			&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
			&user.CreatedAt, &user.UpdatedAt,
		)
		if err != nil {
			utils.LogPrintf("[USER] ERROR: Failed to scan user: error=%v", err)
			continue
		}
		users = append(users, user)
	}

	return users, total, nil
}

// GetStats 获取用户统计数据
// 参数：
//   - ctx: 上下文
//
// 返回：
//   - *UserStats: 统计数据
//   - error: 错误信息
func (r *UserRepository) GetStats(ctx context.Context) (*UserStats, error) {
	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, err
	}

	stats := &UserStats{}

	// 总用户数
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&stats.TotalUsers)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to count total users: error=%v", err)
		return nil, fmt.Errorf("count total users failed: %w", err)
	}

	// 今日新增用户
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users 
		WHERE created_at >= CURRENT_DATE
	`).Scan(&stats.TodayNewUsers)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to count today users: error=%v", err)
		return nil, fmt.Errorf("count today users failed: %w", err)
	}

	// 管理员数量
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users WHERE role >= $1
	`, RoleAdmin).Scan(&stats.AdminCount)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to count admins: error=%v", err)
		return nil, fmt.Errorf("count admins failed: %w", err)
	}

	// 微软账户绑定数
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users WHERE microsoft_id IS NOT NULL
	`).Scan(&stats.MicrosoftLinked)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to count microsoft linked: error=%v", err)
		return nil, fmt.Errorf("count microsoft linked failed: %w", err)
	}

	// 封禁用户数
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users WHERE is_banned = true
	`).Scan(&stats.BannedCount)
	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to count banned users: error=%v", err)
		return nil, fmt.Errorf("count banned users failed: %w", err)
	}

	return stats, nil
}


// ====================  封禁管理方法 ====================

// Ban 封禁用户
// 参数：
//   - ctx: 上下文
//   - userID: 被封禁用户 ID
//   - adminID: 操作管理员 ID
//   - reason: 封禁原因
//   - unbanAt: 解封时间（nil 表示永久封禁）
//
// 返回：
//   - error: 错误信息
func (r *UserRepository) Ban(ctx context.Context, userID, adminID int64, reason string, unbanAt *time.Time) error {
	// 参数验证
	if userID <= 0 {
		return ErrUserRepoInvalidID
	}
	if adminID <= 0 {
		return fmt.Errorf("%w: invalid admin ID", ErrInvalidUserData)
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return err
	}

	// 执行封禁
	result, err := pool.Exec(ctx, `
		UPDATE users SET 
			is_banned = true,
			ban_reason = $1,
			banned_at = CURRENT_TIMESTAMP,
			banned_by = $2,
			unban_at = $3,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $4
	`, reason, adminID, unbanAt, userID)

	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to ban user: id=%d, error=%v", userID, err)
		return fmt.Errorf("ban user failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	utils.LogPrintf("[USER] User banned: id=%d, admin_id=%d, reason=%s", userID, adminID, reason)
	return nil
}

// Unban 解封用户
// 参数：
//   - ctx: 上下文
//   - userID: 被解封用户 ID
//
// 返回：
//   - error: 错误信息
func (r *UserRepository) Unban(ctx context.Context, userID int64) error {
	// 参数验证
	if userID <= 0 {
		return ErrUserRepoInvalidID
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return err
	}

	// 执行解封
	result, err := pool.Exec(ctx, `
		UPDATE users SET 
			is_banned = false,
			ban_reason = NULL,
			banned_at = NULL,
			banned_by = NULL,
			unban_at = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, userID)

	if err != nil {
		utils.LogPrintf("[USER] ERROR: Failed to unban user: id=%d, error=%v", userID, err)
		return fmt.Errorf("unban user failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	utils.LogPrintf("[USER] User unbanned: id=%d", userID)
	return nil
}
