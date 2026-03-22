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
	"auth-system/internal/config"
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
	// ErrEmailExists 邮箱已存在
	ErrEmailExists = errors.New("EMAIL_EXISTS")
	// ErrUsernameExists 用户名已存在
	ErrUsernameExists = errors.New("USERNAME_EXISTS")
	// ErrMicrosoftIDExists Microsoft ID 已存在
	ErrMicrosoftIDExists = errors.New("MICROSOFT_ID_EXISTS")
)

// ====================  常量定义 ====================

const (
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
	UID                 string         `json:"uid"`
	Username            string         `json:"username"`
	Email               string         `json:"email"`
	Password            string         `json:"-"` // 不序列化到 JSON
	AvatarURL           string         `json:"avatar_url"`
	Role                int            `json:"role"` // 0: user, 1: admin, 2: super_admin
	MicrosoftID         sql.NullString `json:"microsoft_id"`
	MicrosoftName       sql.NullString `json:"microsoft_name"`
	MicrosoftAvatarURL  sql.NullString `json:"microsoft_avatar_url"`
	MicrosoftAvatarHash sql.NullString `json:"-"`          // 头像哈希，用于判断是否需要更新
	IsBanned            bool           `json:"is_banned"`  // 是否被封禁
	BanReason           sql.NullString `json:"ban_reason"` // 封禁原因
	BannedAt            sql.NullTime   `json:"banned_at"`  // 封禁时间
	BannedBy            sql.NullString `json:"banned_by"`  // 封禁操作者 UID
	UnbanAt             sql.NullTime   `json:"unban_at"`   // 解封时间（NULL 表示永封）
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

// UserPublic 公开的用户信息（不含敏感数据）
type UserPublic struct {
	ID                 int64      `json:"id"`
	UID                string     `json:"uid"`
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
		UID:       u.UID,
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
		return errors.New("user object is nil")
	}
	if u.Username == "" {
		return errors.New("username is empty")
	}
	if u.Email == "" {
		return errors.New("email is empty")
	}
	if u.Password == "" {
		return errors.New("password is empty")
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
	if id <= 0 {
		return nil, errors.New("invalid user ID")
	}

	if pool == nil {
		return nil, errors.New("database not ready")
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, uid, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE id = $1
	`, id).Scan(
		&user.ID, &user.UID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, utils.HandleDatabaseError("USER", "FindByID", err, id)
	}

	return user, nil
}

// FindByUID 根据 UID 查找用户
// 参数：
//   - ctx: 上下文
//   - uid: 用户 UID
//
// 返回：
//   - *User: 用户对象
//   - error: 错误信息
func (r *UserRepository) FindByUID(ctx context.Context, uid string) (*User, error) {
	if uid == "" {
		return nil, errors.New("invalid user UID")
	}

	if pool == nil {
		return nil, errors.New("database not ready")
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, uid, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE uid = $1
	`, uid).Scan(
		&user.ID, &user.UID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, utils.HandleDatabaseError("USER", "FindByUID", err, uid)
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
	if identifier == "" {
		return nil, errors.New("empty identifier")
	}

	if pool == nil {
		return nil, errors.New("database not ready")
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, uid, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE email = $1 OR username = $1
		LIMIT 1
	`, identifier).Scan(
		&user.ID, &user.UID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, utils.HandleDatabaseError("USER", "FindByEmailOrUsername", err, identifier)
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
	if email == "" {
		return nil, errors.New("empty email")
	}

	if pool == nil {
		return nil, errors.New("database not ready")
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, uid, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE email = $1
	`, email).Scan(
		&user.ID, &user.UID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, utils.HandleDatabaseError("USER", "FindByEmail", err, email)
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
	if username == "" {
		return nil, errors.New("empty username")
	}

	if pool == nil {
		return nil, errors.New("database not ready")
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, uid, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE username = $1
	`, username).Scan(
		&user.ID, &user.UID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, utils.HandleDatabaseError("USER", "FindByUsername", err, username)
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
	if msID == "" {
		return nil, errors.New("empty microsoft ID")
	}

	if pool == nil {
		return nil, errors.New("database not ready")
	}

	user := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, uid, username, email, password, avatar_url, role,
		       microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at,
		       created_at, updated_at
		FROM users WHERE microsoft_id = $1
	`, msID).Scan(
		&user.ID, &user.UID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
		&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
		&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, utils.HandleDatabaseError("USER", "FindByMicrosoftID", err, msID)
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
	if user == nil {
		return errors.New("user object is nil")
	}

	if err := user.Validate(); err != nil {
		return err
	}

	if pool == nil {
		return errors.New("database not ready")
	}

	// 设置默认头像
	if user.AvatarURL == "" {
		user.AvatarURL = config.Get().DefaultAvatarURL
	}

	// 设置默认角色（普通用户）
	if user.Role == 0 {
		user.Role = RoleUser
	}

	// 生成 UID
	if user.UID == "" {
		var err error
		user.UID, err = utils.GenerateUID()
		if err != nil {
			return utils.LogError("USER", "Create", err, "Failed to generate UID")
		}
	}

	// 执行插入
	err := pool.QueryRow(ctx, `
		INSERT INTO users (uid, username, email, password, avatar_url, microsoft_id, role)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`, user.UID, user.Username, user.Email, user.Password, user.AvatarURL, user.MicrosoftID, user.Role).Scan(
		&user.ID, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return r.handleWriteError(err, "Create", user.Email)
	}

	utils.LogInfo("USER", fmt.Sprintf("User created: id=%d, email=%s", user.ID, user.Email))
	return nil
}

// Update 更新用户
// 参数：
//   - ctx: 上下文
//   - uid: 用户 UID
//   - updates: 要更新的字段映射
//
// 返回：
//   - error: 错误信息
func (r *UserRepository) Update(ctx context.Context, uid string, updates map[string]any) error {
	if uid == "" {
		return errors.New("invalid user UID")
	}

	if len(updates) == 0 {
		utils.LogWarn("USER", fmt.Sprintf("Update called with empty updates: uid=%s", uid))
		return nil
	}

	if len(updates) > maxUpdateFields {
		return errors.New("too many update fields")
	}

	if pool == nil {
		return errors.New("database not ready")
	}

	// 构建动态 SQL（使用白名单验证字段）
	query, args, err := r.buildUpdateQuery(uid, updates)
	if err != nil {
		return err
	}

	// 执行更新
	result, err := pool.Exec(ctx, query, args...)
	if err != nil {
		return r.handleWriteError(err, "Update", uid)
	}

	if result.RowsAffected() == 0 {
		return utils.HandleDatabaseError("USER", "Update", errors.New("no rows affected"), uid)
	}

	utils.LogInfo("USER", fmt.Sprintf("User updated: uid=%s, fields=%d", uid, len(updates)))
	return nil
}

// Delete 删除用户
// 参数：
//   - ctx: 上下文
//   - uid: 用户 UID
//
// 返回：
//   - error: 错误信息
func (r *UserRepository) Delete(ctx context.Context, uid string) error {
	if uid == "" {
		return errors.New("invalid user UID")
	}

	if pool == nil {
		return errors.New("database not ready")
	}

	result, err := pool.Exec(ctx, "DELETE FROM users WHERE uid = $1", uid)
	if err != nil {
		return utils.LogError("USER", "Delete", err, fmt.Sprintf("uid=%s", uid))
	}

	if result.RowsAffected() == 0 {
		return utils.HandleDatabaseError("USER", "Delete", errors.New("no rows affected"), uid)
	}

	utils.LogInfo("USER", fmt.Sprintf("User deleted: uid=%s", uid))
	return nil
}

// ====================  管理后台方法 ====================

// handleWriteError 处理写入错误
// 参数：
//   - err: 原始错误
//   - operation: 操作名称
//   - identifier: 相关标识符
//
// 返回：
//   - error: 处理后的错误
func (r *UserRepository) handleWriteError(err error, operation string, identifier any) error {
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

	// 使用统一的错误日志记录
	return utils.LogError("USER", operation, err, fmt.Sprintf("identifier=%v", identifier))
}

// buildUpdateQuery 构建更新 SQL 查询
// 参数：
//   - uid: 用户 UID
//   - updates: 要更新的字段映射
//
// 返回：
//   - string: SQL 查询
//   - []interface{}: 参数列表
//   - error: 错误信息
func (r *UserRepository) buildUpdateQuery(uid string, updates map[string]any) (string, []any, error) {
	var setClauses []string
	args := make([]any, 0, len(updates)+1)
	argIndex := 1

	for key, value := range updates {
		// 验证字段是否在白名单中（防止 SQL 注入）
		if !allowedUpdateFields[key] {
			utils.LogWarn("USER", fmt.Sprintf("Attempted to update disallowed field: %s", key))
			continue
		}

		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", key, argIndex))
		args = append(args, value)
		argIndex++
	}

	if len(setClauses) == 0 {
		return "", nil, errors.New("no valid fields to update")
	}

	// 添加 updated_at
	query := fmt.Sprintf(
		"UPDATE users SET updated_at = CURRENT_TIMESTAMP, %s WHERE uid = $%d",
		strings.Join(setClauses, ", "),
		argIndex,
	)
	args = append(args, uid)

	return query, args, nil
}

// ====================  管理后台方法 ====================

// UserStats 用户统计数据
type UserStats struct {
	TotalUsers    int64 `json:"totalUsers"`
	TodayNewUsers int64 `json:"todayNewUsers"`
	AdminCount    int64 `json:"adminCount"`
	BannedCount   int64 `json:"bannedCount"`
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
	if pool == nil {
		return nil, 0, errors.New("database not ready")
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
			return nil, 0, utils.LogError("USER", "CountUsers", err)
		}

		rows, err = pool.Query(ctx, `
			SELECT id, uid, username, email, password, avatar_url, role,
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
			return nil, 0, utils.LogError("USER", "CountUsersWithSearch", err)
		}

		rows, err = pool.Query(ctx, `
			SELECT id, uid, username, email, password, avatar_url, role,
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
		return nil, 0, utils.LogError("USER", "QueryUsers", err)
	}
	defer rows.Close()

	// 扫描结果
	users := make([]*User, 0)
	pgxRows := rows.(interface {
		Next() bool
		Scan(dest ...any) error
	})

	for pgxRows.Next() {
		user := &User{}
		err := pgxRows.Scan(
			&user.ID, &user.UID, &user.Username, &user.Email, &user.Password, &user.AvatarURL, &user.Role,
			&user.MicrosoftID, &user.MicrosoftName, &user.MicrosoftAvatarURL, &user.MicrosoftAvatarHash,
			&user.IsBanned, &user.BanReason, &user.BannedAt, &user.BannedBy, &user.UnbanAt,
			&user.CreatedAt, &user.UpdatedAt,
		)
		if err != nil {
			utils.LogWarn("USER", fmt.Sprintf("Failed to scan user: %v", err))
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
	if pool == nil {
		return nil, errors.New("database not ready")
	}

	stats := &UserStats{}

	// 总用户数
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&stats.TotalUsers)
	if err != nil {
		return nil, utils.LogError("USER", "CountTotalUsers", err)
	}

	// 今日新增用户
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users 
		WHERE created_at >= CURRENT_DATE
	`).Scan(&stats.TodayNewUsers)
	if err != nil {
		return nil, utils.LogError("USER", "CountTodayUsers", err)
	}

	// 管理员数量
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users WHERE role >= $1
	`, RoleAdmin).Scan(&stats.AdminCount)
	if err != nil {
		return nil, utils.LogError("USER", "CountAdmins", err)
	}

	// 封禁用户数
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users WHERE is_banned = true
	`).Scan(&stats.BannedCount)
	if err != nil {
		return nil, utils.LogError("USER", "CountBannedUsers", err)
	}

	return stats, nil
}

// Ban 封禁用户
// 参数：
//   - ctx: 上下文
//   - userUID: 被封禁用户 UID
//   - adminUID: 操作管理员 UID
//   - reason: 封禁原因
//   - unbanAt: 解封时间（nil 表示永久封禁）
//
// 返回：
//   - error: 错误信息
func (r *UserRepository) Ban(ctx context.Context, userUID, adminUID string, reason string, unbanAt *time.Time) error {
	if userUID == "" {
		return errors.New("invalid user UID")
	}
	if adminUID == "" {
		return errors.New("invalid admin UID")
	}

	if pool == nil {
		return errors.New("database not ready")
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
		WHERE uid = $4
	`, reason, adminUID, unbanAt, userUID)

	if err != nil {
		return utils.LogError("USER", "Ban", err, fmt.Sprintf("userUID=%s", userUID))
	}

	if result.RowsAffected() == 0 {
		return utils.HandleDatabaseError("USER", "Ban", errors.New("no rows affected"), userUID)
	}

	utils.LogInfo("USER", fmt.Sprintf("User banned: uid=%s, admin_uid=%s, reason=%s", userUID, adminUID, reason))
	return nil
}

// Unban 解封用户
// 参数：
//   - ctx: 上下文
//   - userUID: 被解封用户 UID
//
// 返回：
//   - error: 错误信息
func (r *UserRepository) Unban(ctx context.Context, userUID string) error {
	if userUID == "" {
		return errors.New("invalid user UID")
	}

	if pool == nil {
		return errors.New("database not ready")
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
		WHERE uid = $1
	`, userUID)

	if err != nil {
		return utils.LogError("USER", "Unban", err, fmt.Sprintf("userUID=%s", userUID))
	}

	if result.RowsAffected() == 0 {
		return utils.HandleDatabaseError("USER", "Unban", errors.New("no rows affected"), userUID)
	}

	utils.LogInfo("USER", fmt.Sprintf("User unbanned: uid=%s", userUID))
	return nil
}
