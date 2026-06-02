package models

import (
	"auth-system/internal/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAdminLogDBNotReady  = errors.New("database not ready")
	ErrAdminLogInvalidData = errors.New("invalid admin log data")
)

const (
	ActionSetRole                     = "set_role"
	ActionDeleteUser                  = "delete_user"
	ActionBanUser                     = "ban_user"
	ActionUnbanUser                   = "unban_user"
	ActionOAuthClientCreate           = "oauth_client_create"
	ActionOAuthClientUpdate           = "oauth_client_update"
	ActionOAuthClientDelete           = "oauth_client_delete"
	ActionOAuthClientRegenerateSecret = "oauth_client_regenerate_secret"
	ActionOAuthClientToggle           = "oauth_client_toggle"

	ActionEmailWhitelistCreate = "email_whitelist_create"
	ActionEmailWhitelistUpdate = "email_whitelist_update"
	ActionEmailWhitelistDelete = "email_whitelist_delete"

	ActionDataExport = "data_export"
	ActionDataImport = "data_import"
)

// AdminLog 管理员操作日志
type AdminLog struct {
	ID        int64           `json:"id"`
	AdminUID  string          `json:"admin_uid"`
	Action    string          `json:"action"`
	TargetUID *string         `json:"target_uid,omitempty"`
	Details   json.RawMessage `json:"details,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// AdminLogPublic 公开的日志信息（含管理员用户名）
type AdminLogPublic struct {
	ID            int64           `json:"id"`
	AdminUID      string          `json:"admin_uid"`
	AdminUsername string          `json:"admin_username"`
	Action        string          `json:"action"`
	TargetUID     *string         `json:"target_uid,omitempty"`
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

// DataExportDetails 导出数据操作详情
type DataExportDetails struct {
	UsersCount int `json:"users_count"`
	LogsCount  int `json:"logs_count"`
}

// DataImportDetails 导入数据操作详情
type DataImportDetails struct {
	UsersImported int `json:"users_imported"`
	LogsImported  int `json:"logs_imported"`
}

// AdminLogRepository 管理员日志仓库
type AdminLogRepository struct {
	pool *pgxpool.Pool
}

// NewAdminLogRepository 创建管理员日志仓库
func NewAdminLogRepository(pool *pgxpool.Pool) *AdminLogRepository {
	return &AdminLogRepository{pool: pool}
}

// Create 创建日志记录
func (r *AdminLogRepository) Create(ctx context.Context, log *AdminLog) error {
	// 参数验证
	if log == nil {
		return ErrAdminLogInvalidData
	}
	if log.AdminUID == "" {
		return fmt.Errorf("%w: admin_uid is required", ErrAdminLogInvalidData)
	}
	if log.Action == "" {
		return fmt.Errorf("%w: action is required", ErrAdminLogInvalidData)
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return err
	}

	// 执行插入
	err := r.pool.QueryRow(ctx, `
		INSERT INTO admin_logs (admin_uid, action, target_uid, details)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`, log.AdminUID, log.Action, log.TargetUID, log.Details).Scan(
		&log.ID, &log.CreatedAt,
	)

	if err != nil {
		utils.LogError("ADMIN_LOG", "Create", err, "Failed to create log")
		return fmt.Errorf("create admin log failed: %w", err)
	}

	utils.LogInfo("ADMIN_LOG", fmt.Sprintf("Log created: id=%d, admin_uid=%s, action=%s",
		log.ID, log.AdminUID, log.Action))
	return nil
}

// LogSetRole 记录修改角色操作
func (r *AdminLogRepository) LogSetRole(ctx context.Context, adminUID, targetUID string, targetUsername string, oldRole, newRole int) error {
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
		AdminUID:  adminUID,
		Action:    ActionSetRole,
		TargetUID: &targetUID,
		Details:   detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogDeleteUser 记录删除用户操作
func (r *AdminLogRepository) LogDeleteUser(ctx context.Context, adminUID, targetUID string, targetUsername, targetEmail string) error {
	details := DeleteUserDetails{
		TargetUsername: targetUsername,
		TargetEmail:    targetEmail,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminUID:  adminUID,
		Action:    ActionDeleteUser,
		TargetUID: &targetUID,
		Details:   detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogBanUser 记录封禁用户操作
func (r *AdminLogRepository) LogBanUser(ctx context.Context, adminUID, targetUID string, targetUsername, reason string, unbanAt *time.Time) error {
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
		AdminUID:  adminUID,
		Action:    ActionBanUser,
		TargetUID: &targetUID,
		Details:   detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogUnbanUser 记录解封用户操作
func (r *AdminLogRepository) LogUnbanUser(ctx context.Context, adminUID, targetUID string, targetUsername string) error {
	details := UnbanUserDetails{
		TargetUsername: targetUsername,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminUID:  adminUID,
		Action:    ActionUnbanUser,
		TargetUID: &targetUID,
		Details:   detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogOAuthClientCreate 记录创建 OAuth 客户端操作
func (r *AdminLogRepository) LogOAuthClientCreate(ctx context.Context, adminUID string, clientDBID int64, clientID, clientName string) error {
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
		AdminUID: adminUID,
		Action:   ActionOAuthClientCreate,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogOAuthClientUpdate 记录更新 OAuth 客户端操作
func (r *AdminLogRepository) LogOAuthClientUpdate(ctx context.Context, adminUID string, clientDBID int64, clientID, clientName string) error {
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
		AdminUID: adminUID,
		Action:   ActionOAuthClientUpdate,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogOAuthClientDelete 记录删除 OAuth 客户端操作
func (r *AdminLogRepository) LogOAuthClientDelete(ctx context.Context, adminUID string, clientDBID int64, clientID, clientName string) error {
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
		AdminUID: adminUID,
		Action:   ActionOAuthClientDelete,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogOAuthClientRegenerateSecret 记录重新生成 OAuth 客户端密钥操作
func (r *AdminLogRepository) LogOAuthClientRegenerateSecret(ctx context.Context, adminUID string, clientDBID int64, clientID, clientName string) error {
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
		AdminUID: adminUID,
		Action:   ActionOAuthClientRegenerateSecret,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogOAuthClientToggle 记录启用/禁用 OAuth 客户端操作
func (r *AdminLogRepository) LogOAuthClientToggle(ctx context.Context, adminUID string, clientDBID int64, clientID, clientName string, enabled bool) error {
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
		AdminUID: adminUID,
		Action:   ActionOAuthClientToggle,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogEmailWhitelistCreate 记录创建邮箱白名单
func (r *AdminLogRepository) LogEmailWhitelistCreate(ctx context.Context, adminUID string, entry *EmailWhitelist) error {
	details := map[string]any{
		"id":     entry.ID,
		"domain": entry.Domain,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	targetUID := ""
	log := &AdminLog{
		AdminUID:  adminUID,
		Action:    ActionEmailWhitelistCreate,
		TargetUID: &targetUID,
		Details:   detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogEmailWhitelistUpdate 记录更新邮箱白名单
func (r *AdminLogRepository) LogEmailWhitelistUpdate(ctx context.Context, adminUID string, entry *EmailWhitelist) error {
	details := map[string]any{
		"id":         entry.ID,
		"domain":     entry.Domain,
		"signup_url": entry.SignupURL,
		"is_enabled": entry.IsEnabled,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	targetUID := ""
	log := &AdminLog{
		AdminUID:  adminUID,
		Action:    ActionEmailWhitelistUpdate,
		TargetUID: &targetUID,
		Details:   detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogEmailWhitelistDelete 记录删除邮箱白名单
func (r *AdminLogRepository) LogEmailWhitelistDelete(ctx context.Context, adminUID string, id int64) error {
	details := map[string]any{
		"id": id,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	targetUID := ""
	log := &AdminLog{
		AdminUID:  adminUID,
		Action:    ActionEmailWhitelistDelete,
		TargetUID: &targetUID,
		Details:   detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogDataExport 记录数据导出操作
func (r *AdminLogRepository) LogDataExport(ctx context.Context, adminUID string, usersCount, logsCount int) error {
	details := DataExportDetails{
		UsersCount: usersCount,
		LogsCount:  logsCount,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminUID: adminUID,
		Action:   ActionDataExport,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// LogDataImport 记录数据导入操作
func (r *AdminLogRepository) LogDataImport(ctx context.Context, adminUID string, usersImported, logsImported int) error {
	details := DataImportDetails{
		UsersImported: usersImported,
		LogsImported:  logsImported,
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &AdminLog{
		AdminUID: adminUID,
		Action:   ActionDataImport,
		Details:  detailsJSON,
	}

	return r.Create(ctx, log)
}

// FindAll 查询日志列表（分页）
func (r *AdminLogRepository) FindAll(ctx context.Context, page, pageSize int) ([]*AdminLogPublic, int64, error) {
	if err := r.checkDB(); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize

	var total int64
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_logs").Scan(&total)
	if err != nil {
		return nil, 0, utils.LogError("ADMIN_LOG", "FindAll.Count", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT l.id, l.admin_uid, u.username, l.action, l.target_uid, l.details, l.created_at
		FROM admin_logs l
		LEFT JOIN users u ON l.admin_uid = u.uid
		ORDER BY l.id DESC
		LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		return nil, 0, utils.LogError("ADMIN_LOG", "FindAll.Query", err)
	}
	defer rows.Close()

	logs := make([]*AdminLogPublic, 0)
	for rows.Next() {
		log := &AdminLogPublic{}
		var adminUsername *string
		err := rows.Scan(
			&log.ID, &log.AdminUID, &adminUsername, &log.Action,
			&log.TargetUID, &log.Details, &log.CreatedAt,
		)
		if err != nil {
			utils.LogError("ADMIN_LOG", "FindAll.Scan", err)
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

// checkDB 检查数据库连接是否就绪
func (r *AdminLogRepository) checkDB() error {
	if r.pool == nil {
		utils.LogError("ADMIN_LOG", "checkDB", ErrAdminLogDBNotReady)
		return ErrAdminLogDBNotReady
	}
	return nil
}
