/**
 * internal/models/admin_log.go
 * 管理员操作日志模型和数据访问层
 *
 * 功能：
 * - 管理员操作日志记录
 * - 日志查询（分页）
 * - JSON 灵活存储详情
 *
 * 依赖：
 * - PostgreSQL 数据库连接池
 */

package models

import (
	"auth-system/internal/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ====================  错误定义 ====================

var (
	// ErrAdminLogDBNotReady 数据库未就绪
	ErrAdminLogDBNotReady = errors.New("database not ready")
	// ErrAdminLogInvalidData 无效的日志数据
	ErrAdminLogInvalidData = errors.New("invalid admin log data")
)

// ====================  常量定义 ====================

const (
	// ActionSetRole 修改用户角色
	ActionSetRole = "set_role"
	// ActionDeleteUser 删除用户
	ActionDeleteUser = "delete_user"
	// ActionBanUser 封禁用户
	ActionBanUser = "ban_user"
	// ActionUnbanUser 解封用户
	ActionUnbanUser = "unban_user"
	// ActionOAuthClientCreate 创建 OAuth 客户端
	ActionOAuthClientCreate = "oauth_client_create"
	// ActionOAuthClientUpdate 更新 OAuth 客户端
	ActionOAuthClientUpdate = "oauth_client_update"
	// ActionOAuthClientDelete 删除 OAuth 客户端
	ActionOAuthClientDelete = "oauth_client_delete"
	// ActionOAuthClientRegenerateSecret 重新生成 OAuth 客户端密钥
	ActionOAuthClientRegenerateSecret = "oauth_client_regenerate_secret"
	// ActionOAuthClientToggle 启用/禁用 OAuth 客户端
	ActionOAuthClientToggle = "oauth_client_toggle"
)

// ====================  数据结构 ====================

// AdminLog 管理员操作日志
type AdminLog struct {
	ID        int64           `json:"id"`
	AdminID   int64           `json:"admin_id"`
	Action    string          `json:"action"`
	TargetID  *int64          `json:"target_id,omitempty"`
	Details   json.RawMessage `json:"details,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// AdminLogPublic 公开的日志信息（含管理员用户名）
type AdminLogPublic struct {
	ID            int64           `json:"id"`
	AdminID       int64           `json:"admin_id"`
	AdminUsername string          `json:"admin_username"`
	Action        string          `json:"action"`
	TargetID      *int64          `json:"target_id,omitempty"`
	Details       json.RawMessage `json:"details,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

// SetRoleDetails 修改角色操作详情
type SetRoleDetails struct {
	TargetUsername string `json:"target_username"`
	OldRole        int    `json:"old_role"`
	NewRole        int    `json:"new_role"`
}

// DeleteUserDetails 删除用户操作详情
type DeleteUserDetails struct {
	TargetUsername string `json:"target_username"`
	TargetEmail    string `json:"target_email"`
}

// BanUserDetails 封禁用户操作详情
type BanUserDetails struct {
	TargetUsername string     `json:"target_username"`
	Reason         string     `json:"reason"`
	UnbanAt        *time.Time `json:"unban_at,omitempty"` // nil 表示永久封禁
}

// UnbanUserDetails 解封用户操作详情
type UnbanUserDetails struct {
	TargetUsername string `json:"target_username"`
}

// OAuthClientDetails OAuth 客户端操作详情
type OAuthClientDetails struct {
	ClientDBID int64  `json:"client_db_id"`
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name"`
}

// OAuthClientToggleDetails OAuth 客户端启用/禁用操作详情
type OAuthClientToggleDetails struct {
	ClientDBID int64  `json:"client_db_id"`
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name"`
	Enabled    bool   `json:"enabled"`
}

// AdminLogRepository 管理员日志仓库
type AdminLogRepository struct{}

// ====================  构造函数 ====================

// NewAdminLogRepository 创建管理员日志仓库
// 返回：
//   - *AdminLogRepository: 日志仓库实例
func NewAdminLogRepository() *AdminLogRepository {
	return &AdminLogRepository{}
}

// ====================  写入方法 ====================

// Create 创建日志记录
// 参数：
//   - ctx: 上下文
//   - log: 日志对象
//
// 返回：
//   - error: 错误信息
func (r *AdminLogRepository) Create(ctx context.Context, log *AdminLog) error {
	// 参数验证
	if log == nil {
		return ErrAdminLogInvalidData
	}
	if log.AdminID <= 0 {
		return fmt.Errorf("%w: admin_id is required", ErrAdminLogInvalidData)
	}
	if log.Action == "" {
		return fmt.Errorf("%w: action is required", ErrAdminLogInvalidData)
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return err
	}

	// 执行插入
	err := pool.QueryRow(ctx, `
		INSERT INTO admin_logs (admin_id, action, target_id, details)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`, log.AdminID, log.Action, log.TargetID, log.Details).Scan(
		&log.ID, &log.CreatedAt,
	)

	if err != nil {
		utils.LogPrintf("[ADMIN_LOG] ERROR: Failed to create log: error=%v", err)
		return fmt.Errorf("create admin log failed: %w", err)
	}

	utils.LogPrintf("[ADMIN_LOG] Log created: id=%d, admin_id=%d, action=%s",
		log.ID, log.AdminID, log.Action)
	return nil
}

// LogSetRole 记录修改角色操作
// 参数：
//   - ctx: 上下文
//   - adminID: 操作者 ID
//   - targetID: 目标用户 ID
//   - targetUsername: 目标用户名
//   - oldRole: 旧角色
//   - newRole: 新角色
//
// 返回：
//   - error: 错误信息
func (r *AdminLogRepository) LogSetRole(ctx context.Context, adminID, targetID int64, targetUsername string, oldRole, newRole int) error {
	details := SetRoleDetails{
		TargetUsername: targetUsername,
		OldRole:        oldRole,
		NewRole:        newRole,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminID:  adminID,
		Action:   ActionSetRole,
		TargetID: &targetID,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogDeleteUser 记录删除用户操作
// 参数：
//   - ctx: 上下文
//   - adminID: 操作者 ID
//   - targetID: 目标用户 ID
//   - targetUsername: 目标用户名
//   - targetEmail: 目标用户邮箱
//
// 返回：
//   - error: 错误信息
func (r *AdminLogRepository) LogDeleteUser(ctx context.Context, adminID, targetID int64, targetUsername, targetEmail string) error {
	details := DeleteUserDetails{
		TargetUsername: targetUsername,
		TargetEmail:    targetEmail,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminID:  adminID,
		Action:   ActionDeleteUser,
		TargetID: &targetID,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogBanUser 记录封禁用户操作
// 参数：
//   - ctx: 上下文
//   - adminID: 操作者 ID
//   - targetID: 目标用户 ID
//   - targetUsername: 目标用户名
//   - reason: 封禁原因
//   - unbanAt: 解封时间（nil 表示永久封禁）
//
// 返回：
//   - error: 错误信息
func (r *AdminLogRepository) LogBanUser(ctx context.Context, adminID, targetID int64, targetUsername, reason string, unbanAt *time.Time) error {
	details := BanUserDetails{
		TargetUsername: targetUsername,
		Reason:         reason,
		UnbanAt:        unbanAt,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminID:  adminID,
		Action:   ActionBanUser,
		TargetID: &targetID,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogUnbanUser 记录解封用户操作
// 参数：
//   - ctx: 上下文
//   - adminID: 操作者 ID
//   - targetID: 目标用户 ID
//   - targetUsername: 目标用户名
//
// 返回：
//   - error: 错误信息
func (r *AdminLogRepository) LogUnbanUser(ctx context.Context, adminID, targetID int64, targetUsername string) error {
	details := UnbanUserDetails{
		TargetUsername: targetUsername,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminID:  adminID,
		Action:   ActionUnbanUser,
		TargetID: &targetID,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogOAuthClientCreate 记录创建 OAuth 客户端操作
func (r *AdminLogRepository) LogOAuthClientCreate(ctx context.Context, adminID, clientDBID int64, clientID, clientName string) error {
	details := OAuthClientDetails{
		ClientDBID: clientDBID,
		ClientID:   clientID,
		ClientName: clientName,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminID: adminID,
		Action:  ActionOAuthClientCreate,
		Details: detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogOAuthClientUpdate 记录更新 OAuth 客户端操作
func (r *AdminLogRepository) LogOAuthClientUpdate(ctx context.Context, adminID, clientDBID int64, clientID, clientName string) error {
	details := OAuthClientDetails{
		ClientDBID: clientDBID,
		ClientID:   clientID,
		ClientName: clientName,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminID: adminID,
		Action:  ActionOAuthClientUpdate,
		Details: detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogOAuthClientDelete 记录删除 OAuth 客户端操作
func (r *AdminLogRepository) LogOAuthClientDelete(ctx context.Context, adminID, clientDBID int64, clientID, clientName string) error {
	details := OAuthClientDetails{
		ClientDBID: clientDBID,
		ClientID:   clientID,
		ClientName: clientName,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminID: adminID,
		Action:  ActionOAuthClientDelete,
		Details: detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogOAuthClientRegenerateSecret 记录重新生成 OAuth 客户端密钥操作
func (r *AdminLogRepository) LogOAuthClientRegenerateSecret(ctx context.Context, adminID, clientDBID int64, clientID, clientName string) error {
	details := OAuthClientDetails{
		ClientDBID: clientDBID,
		ClientID:   clientID,
		ClientName: clientName,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminID: adminID,
		Action:  ActionOAuthClientRegenerateSecret,
		Details: detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogOAuthClientToggle 记录启用/禁用 OAuth 客户端操作
func (r *AdminLogRepository) LogOAuthClientToggle(ctx context.Context, adminID, clientDBID int64, clientID, clientName string, enabled bool) error {
	details := OAuthClientToggleDetails{
		ClientDBID: clientDBID,
		ClientID:   clientID,
		ClientName: clientName,
		Enabled:    enabled,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminID: adminID,
		Action:  ActionOAuthClientToggle,
		Details: detailsJSON,
	}

	return r.Create(ctx, log)
}

// ====================  查询方法 ====================

// FindAll 查询日志列表（分页）
// 参数：
//   - ctx: 上下文
//   - page: 页码（从 1 开始）
//   - pageSize: 每页数量
//
// 返回：
//   - []*AdminLogPublic: 日志列表
//   - int64: 总数
//   - error: 错误信息
func (r *AdminLogRepository) FindAll(ctx context.Context, page, pageSize int) ([]*AdminLogPublic, int64, error) {
	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, 0, err
	}

	// 计算偏移量
	offset := (page - 1) * pageSize

	// 查询总数
	var total int64
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_logs").Scan(&total)
	if err != nil {
		utils.LogPrintf("[ADMIN_LOG] ERROR: Failed to count logs: error=%v", err)
		return nil, 0, fmt.Errorf("count admin logs failed: %w", err)
	}

	// 查询日志列表（关联用户表获取管理员用户名）
	rows, err := pool.Query(ctx, `
		SELECT l.id, l.admin_id, u.username, l.action, l.target_id, l.details, l.created_at
		FROM admin_logs l
		LEFT JOIN users u ON l.admin_id = u.id
		ORDER BY l.id DESC
		LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		utils.LogPrintf("[ADMIN_LOG] ERROR: Failed to query logs: error=%v", err)
		return nil, 0, fmt.Errorf("query admin logs failed: %w", err)
	}
	defer rows.Close()

	// 扫描结果
	logs := make([]*AdminLogPublic, 0)
	for rows.Next() {
		log := &AdminLogPublic{}
		var adminUsername *string
		err := rows.Scan(
			&log.ID, &log.AdminID, &adminUsername, &log.Action,
			&log.TargetID, &log.Details, &log.CreatedAt,
		)
		if err != nil {
			utils.LogPrintf("[ADMIN_LOG] ERROR: Failed to scan log: error=%v", err)
			continue
		}
		if adminUsername != nil {
			log.AdminUsername = *adminUsername
		} else {
			log.AdminUsername = "已删除"
		}
		logs = append(logs, log)
	}

	return logs, total, nil
}

// ====================  私有方法 ====================

// checkDB 检查数据库连接是否就绪
func (r *AdminLogRepository) checkDB() error {
	if pool == nil {
		utils.LogPrintf("[ADMIN_LOG] ERROR: Database pool is nil")
		return ErrAdminLogDBNotReady
	}
	return nil
}
