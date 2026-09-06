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
	ActionResetUserTOTP               = "reset_user_totp"
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

// FieldChange 字段级变更（old/new 为任意 JSON 值；null 表示原本无值）
type FieldChange struct {
	Old any `json:"old"`
	New any `json:"new"`
}

// makeDetails 组装统一信封的 details：
//
//	{
//	  "summary": { 动作对象的识别信息 },
//	  "changes": { "field": {"old": x, "new": y} }  // 仅变更类动作
//	}
//
// changes 为空时省略该键。
func makeDetails(summary any, changes map[string]FieldChange) (json.RawMessage, error) {
	envelope := map[string]any{"summary": summary}
	if len(changes) > 0 {
		envelope["changes"] = changes
	}
	b, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal details failed: %w", err)
	}
	return b, nil
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

	utils.LogInfo("ADMIN_LOG", "Log created", "id", log.ID, "admin_uid", log.AdminUID, "action", log.Action)
	return nil
}

// ---------- 用户动作（target_uid = 被操作用户的 UID） ----------

// LogSetRole 记录修改角色操作
func (r *AdminLogRepository) LogSetRole(ctx context.Context, adminUID, targetUID, targetUsername string, oldRole, newRole int) error {
	summary := map[string]any{"target_username": targetUsername}
	changes := map[string]FieldChange{"role": {Old: oldRole, New: newRole}}

	detailsJSON, err := makeDetails(summary, changes)
	if err != nil {
		return err
	}

	target := targetUID
	log := &AdminLog{AdminUID: adminUID, Action: ActionSetRole, TargetUID: &target, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogDeleteUser 记录删除用户操作
func (r *AdminLogRepository) LogDeleteUser(ctx context.Context, adminUID, targetUID, targetUsername, targetEmail string) error {
	summary := map[string]any{"target_username": targetUsername, "target_email": targetEmail}

	detailsJSON, err := makeDetails(summary, nil)
	if err != nil {
		return err
	}

	target := targetUID
	log := &AdminLog{AdminUID: adminUID, Action: ActionDeleteUser, TargetUID: &target, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogBanUser 记录封禁用户操作
func (r *AdminLogRepository) LogBanUser(ctx context.Context, adminUID, targetUID, targetUsername, reason string, unbanAt *time.Time) error {
	summary := map[string]any{"target_username": targetUsername, "reason": reason, "unban_at": unbanAt}

	detailsJSON, err := makeDetails(summary, nil)
	if err != nil {
		return err
	}

	target := targetUID
	log := &AdminLog{AdminUID: adminUID, Action: ActionBanUser, TargetUID: &target, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogUnbanUser 记录解封用户操作
func (r *AdminLogRepository) LogUnbanUser(ctx context.Context, adminUID, targetUID, targetUsername string) error {
	summary := map[string]any{"target_username": targetUsername}

	detailsJSON, err := makeDetails(summary, nil)
	if err != nil {
		return err
	}

	target := targetUID
	log := &AdminLog{AdminUID: adminUID, Action: ActionUnbanUser, TargetUID: &target, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogResetUserTOTP 记录重置用户两步验证操作
func (r *AdminLogRepository) LogResetUserTOTP(ctx context.Context, adminUID, targetUID, targetUsername string) error {
	summary := map[string]any{"target_username": targetUsername}

	detailsJSON, err := makeDetails(summary, nil)
	if err != nil {
		return err
	}

	target := targetUID
	log := &AdminLog{AdminUID: adminUID, Action: ActionResetUserTOTP, TargetUID: &target, Details: detailsJSON}
	return r.Create(ctx, log)
}

// ---------- OAuth 客户端动作（target_uid 恒为 NULL，对象以 summary 中的 client_id 标识） ----------

func oauthClientSummary(client *OAuthClient) map[string]any {
	return map[string]any{
		"client_db_id": client.ID,
		"client_id":    client.ClientID,
		"name":         client.Name,
	}
}

// LogOAuthClientCreate 记录创建 OAuth 客户端（记录初始配置）
func (r *AdminLogRepository) LogOAuthClientCreate(ctx context.Context, adminUID string, client *OAuthClient) error {
	summary := oauthClientSummary(client)
	summary["redirect_uri"] = client.RedirectURI
	if client.Description != "" {
		summary["description"] = client.Description
	}

	detailsJSON, err := makeDetails(summary, nil)
	if err != nil {
		return err
	}

	log := &AdminLog{AdminUID: adminUID, Action: ActionOAuthClientCreate, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogOAuthClientUpdate 记录更新 OAuth 客户端（changes 记录字段级 old -> new）
func (r *AdminLogRepository) LogOAuthClientUpdate(ctx context.Context, adminUID string, client *OAuthClient, changes map[string]FieldChange) error {
	detailsJSON, err := makeDetails(oauthClientSummary(client), changes)
	if err != nil {
		return err
	}

	log := &AdminLog{AdminUID: adminUID, Action: ActionOAuthClientUpdate, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogOAuthClientDelete 记录删除 OAuth 客户端（记录被删对象的完整标识与回调地址）
func (r *AdminLogRepository) LogOAuthClientDelete(ctx context.Context, adminUID string, client *OAuthClient) error {
	summary := oauthClientSummary(client)
	summary["redirect_uri"] = client.RedirectURI

	detailsJSON, err := makeDetails(summary, nil)
	if err != nil {
		return err
	}

	log := &AdminLog{AdminUID: adminUID, Action: ActionOAuthClientDelete, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogOAuthClientRegenerateSecret 记录重新生成 OAuth 客户端密钥（不记录密钥本身）
func (r *AdminLogRepository) LogOAuthClientRegenerateSecret(ctx context.Context, adminUID string, client *OAuthClient) error {
	detailsJSON, err := makeDetails(oauthClientSummary(client), nil)
	if err != nil {
		return err
	}

	log := &AdminLog{AdminUID: adminUID, Action: ActionOAuthClientRegenerateSecret, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogOAuthClientToggle 记录启用/禁用 OAuth 客户端
func (r *AdminLogRepository) LogOAuthClientToggle(ctx context.Context, adminUID string, client *OAuthClient, oldEnabled bool) error {
	changes := map[string]FieldChange{"is_enabled": {Old: oldEnabled, New: client.IsEnabled}}

	detailsJSON, err := makeDetails(oauthClientSummary(client), changes)
	if err != nil {
		return err
	}

	log := &AdminLog{AdminUID: adminUID, Action: ActionOAuthClientToggle, Details: detailsJSON}
	return r.Create(ctx, log)
}

// ---------- 邮箱白名单动作（target_uid 恒为 NULL，对象以 summary 中的 domain 标识） ----------

func emailWhitelistSummary(entry *EmailWhitelist) map[string]any {
	return map[string]any{"id": entry.ID, "domain": entry.Domain}
}

// LogEmailWhitelistCreate 记录创建邮箱白名单（记录初始配置）
func (r *AdminLogRepository) LogEmailWhitelistCreate(ctx context.Context, adminUID string, entry *EmailWhitelist) error {
	summary := emailWhitelistSummary(entry)
	summary["signup_url"] = entry.SignupURL
	summary["is_enabled"] = entry.IsEnabled
	if entry.LogoURL != "" {
		summary["logo_url"] = entry.LogoURL
	}

	detailsJSON, err := makeDetails(summary, nil)
	if err != nil {
		return err
	}

	log := &AdminLog{AdminUID: adminUID, Action: ActionEmailWhitelistCreate, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogEmailWhitelistUpdate 记录更新邮箱白名单（changes 记录字段级 old -> new：
// 域名、注册链接、徽标、启用状态各自独立记录，未变更的字段不出现）
func (r *AdminLogRepository) LogEmailWhitelistUpdate(ctx context.Context, adminUID string, entry *EmailWhitelist, changes map[string]FieldChange) error {
	detailsJSON, err := makeDetails(emailWhitelistSummary(entry), changes)
	if err != nil {
		return err
	}

	log := &AdminLog{AdminUID: adminUID, Action: ActionEmailWhitelistUpdate, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogEmailWhitelistDelete 记录删除邮箱白名单（记录被删条目的域名）
func (r *AdminLogRepository) LogEmailWhitelistDelete(ctx context.Context, adminUID string, entry *EmailWhitelist) error {
	detailsJSON, err := makeDetails(emailWhitelistSummary(entry), nil)
	if err != nil {
		return err
	}

	log := &AdminLog{AdminUID: adminUID, Action: ActionEmailWhitelistDelete, Details: detailsJSON}
	return r.Create(ctx, log)
}

// ---------- 数据导入导出 ----------

// LogDataExport 记录数据导出操作
func (r *AdminLogRepository) LogDataExport(ctx context.Context, adminUID string, usersCount, logsCount int) error {
	summary := map[string]any{"users_count": usersCount, "logs_count": logsCount}

	detailsJSON, err := makeDetails(summary, nil)
	if err != nil {
		return err
	}

	log := &AdminLog{AdminUID: adminUID, Action: ActionDataExport, Details: detailsJSON}
	return r.Create(ctx, log)
}

// LogDataImport 记录数据导入操作
func (r *AdminLogRepository) LogDataImport(ctx context.Context, adminUID string, usersImported, logsImported int) error {
	summary := map[string]any{"users_imported": usersImported, "logs_imported": logsImported}

	detailsJSON, err := makeDetails(summary, nil)
	if err != nil {
		return err
	}

	log := &AdminLog{AdminUID: adminUID, Action: ActionDataImport, Details: detailsJSON}
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
			// 扫描失败属于编程错误，静默丢行会让分页结果悄悄缺数据
			return nil, 0, fmt.Errorf("failed to scan admin log: %w", err)
		}
		if adminUsername != nil {
			log.AdminUsername = *adminUsername
		} else {
			log.AdminUsername = "已删除"
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("failed to iterate admin logs: %w", err)
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
