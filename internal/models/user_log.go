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

const (
	UserActionRegister          = "register"
	UserActionChangePassword    = "change_password"
	UserActionChangeUsername    = "change_username"
	UserActionChangeAvatar      = "change_avatar"
	UserActionLinkMicrosoft     = "link_microsoft"
	UserActionUnlinkMicrosoft   = "unlink_microsoft"
	UserActionDeleteAccount     = "delete_account"
	UserActionBanned            = "banned"
	UserActionUnbanned          = "unbanned"
	UserActionOAuthAuthorize    = "oauth_authorize"
	UserActionOAuthRevoke       = "oauth_revoke"
)

// UserLog 用户操作日志
type UserLog struct {
	ID        int64           `json:"id"`
	UserUID   string          `json:"user_uid"`
	Action    string          `json:"action"`
	Details   json.RawMessage `json:"details,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// ChangeUsernameDetails 修改用户名详情
type ChangeUsernameDetails struct {
	OldUsername string `json:"old_username"`
	NewUsername string `json:"new_username"`
}

// ChangeAvatarDetails 修改头像详情
type ChangeAvatarDetails struct {
	OldAvatarURL string `json:"old_avatar_url,omitempty"`
	NewAvatarURL string `json:"new_avatar_url,omitempty"`
}

// LinkMicrosoftDetails 绑定微软账户详情
type LinkMicrosoftDetails struct {
	MicrosoftID   string `json:"microsoft_id"`
	MicrosoftName string `json:"microsoft_name"`
}

// UnlinkMicrosoftDetails 解绑微软账户详情
type UnlinkMicrosoftDetails struct {
	MicrosoftID   string `json:"microsoft_id"`
	MicrosoftName string `json:"microsoft_name"`
}

// BannedDetails 被封禁详情
type BannedDetails struct {
	Reason  string     `json:"reason"`
	UnbanAt *time.Time `json:"unban_at,omitempty"` // nil 表示永久封禁
}

// OAuthAuthorizeDetails OAuth 授权详情
type OAuthAuthorizeDetails struct {
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name"`
	Scope      string `json:"scope"`
}

// OAuthRevokeDetails OAuth 撤销授权详情
type OAuthRevokeDetails struct {
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name"`
}

// UserLogRepository 用户日志仓库
type UserLogRepository struct {
	pool *pgxpool.Pool
}

// NewUserLogRepository 创建用户日志仓库
func NewUserLogRepository(pool *pgxpool.Pool) *UserLogRepository {
	return &UserLogRepository{pool: pool}
}

// Create 创建日志记录
func (r *UserLogRepository) Create(ctx context.Context, log *UserLog) error {
	if log == nil {
		return errors.New("log object is nil")
	}
	if log.UserUID == "" {
		return errors.New("user_uid is required")
	}
	if log.Action == "" {
		return errors.New("action is required")
	}

	if r.pool == nil {
		return errors.New("database not ready")
	}

	err := r.pool.QueryRow(ctx, `
		INSERT INTO user_logs (user_uid, action, details)
		VALUES ($1, $2, $3)
		RETURNING id, created_at
	`, log.UserUID, log.Action, log.Details).Scan(&log.ID, &log.CreatedAt)

	if err != nil {
		return utils.LogError("USER_LOG", "Create", err, fmt.Sprintf("user_uid=%s, action=%s", log.UserUID, log.Action))
	}

	utils.LogInfo("USER_LOG", fmt.Sprintf("Log created: id=%d, user_uid=%s, action=%s", log.ID, log.UserUID, log.Action))
	return nil
}

// LogChangePassword 记录修改密码操作
func (r *UserLogRepository) LogChangePassword(ctx context.Context, userUID string) error {
	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionChangePassword,
	}
	return r.Create(ctx, log)
}

// LogRegister 记录用户注册操作
func (r *UserLogRepository) LogRegister(ctx context.Context, userUID string) error {
	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionRegister,
	}
	return r.Create(ctx, log)
}

// LogChangeUsername 记录修改用户名操作
func (r *UserLogRepository) LogChangeUsername(ctx context.Context, userUID string, oldUsername, newUsername string) error {
	details := ChangeUsernameDetails{
		OldUsername: oldUsername,
		NewUsername: newUsername,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionChangeUsername,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogChangeAvatar 记录修改头像操作
func (r *UserLogRepository) LogChangeAvatar(ctx context.Context, userUID string, oldURL, newURL string) error {
	details := ChangeAvatarDetails{
		OldAvatarURL: oldURL,
		NewAvatarURL: newURL,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionChangeAvatar,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogLinkMicrosoft 记录绑定微软账户操作
func (r *UserLogRepository) LogLinkMicrosoft(ctx context.Context, userUID string, microsoftID, microsoftName string) error {
	details := LinkMicrosoftDetails{
		MicrosoftID:   microsoftID,
		MicrosoftName: microsoftName,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionLinkMicrosoft,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogUnlinkMicrosoft 记录解绑微软账户操作
func (r *UserLogRepository) LogUnlinkMicrosoft(ctx context.Context, userUID string, microsoftID, microsoftName string) error {
	details := UnlinkMicrosoftDetails{
		MicrosoftID:   microsoftID,
		MicrosoftName: microsoftName,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionUnlinkMicrosoft,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogDeleteAccount 记录删除账户操作
func (r *UserLogRepository) LogDeleteAccount(ctx context.Context, userUID string) error {
	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionDeleteAccount,
	}
	return r.Create(ctx, log)
}

// LogBanned 记录被封禁
func (r *UserLogRepository) LogBanned(ctx context.Context, userUID string, reason string, unbanAt *time.Time) error {
	details := BannedDetails{
		Reason:  reason,
		UnbanAt: unbanAt,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionBanned,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogUnbanned 记录被解封
func (r *UserLogRepository) LogUnbanned(ctx context.Context, userUID string) error {
	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionUnbanned,
	}
	return r.Create(ctx, log)
}

// LogOAuthAuthorize 记录 OAuth 授权第三方应用
func (r *UserLogRepository) LogOAuthAuthorize(ctx context.Context, userUID string, clientID, clientName, scope string) error {
	details := OAuthAuthorizeDetails{
		ClientID:   clientID,
		ClientName: clientName,
		Scope:      scope,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionOAuthAuthorize,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogOAuthRevoke 记录 OAuth 撤销第三方应用授权
func (r *UserLogRepository) LogOAuthRevoke(ctx context.Context, userUID string, clientID, clientName string) error {
	details := OAuthRevokeDetails{
		ClientID:   clientID,
		ClientName: clientName,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserUID: userUID,
		Action:  UserActionOAuthRevoke,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// FindByUserUID 查询用户的操作日志（分页）
func (r *UserLogRepository) FindByUserUID(ctx context.Context, userUID string, page, pageSize int) ([]*UserLog, int64, error) {
	if r.pool == nil {
		return nil, 0, errors.New("database not ready")
	}

	offset := (page - 1) * pageSize

	// 查询总数
	var total int64
	err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM user_logs WHERE user_uid = $1",
		userUID,
	).Scan(&total)
	if err != nil {
		return nil, 0, utils.LogError("USER_LOG", "CountLogs", err, fmt.Sprintf("user_uid=%s", userUID))
	}

	// 查询日志列表
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_uid, action, details, created_at
		FROM user_logs
		WHERE user_uid = $1
		ORDER BY id DESC
		LIMIT $2 OFFSET $3
	`, userUID, pageSize, offset)
	if err != nil {
		return nil, 0, utils.LogError("USER_LOG", "QueryLogs", err, fmt.Sprintf("user_uid=%s", userUID))
	}
	defer rows.Close()

	logs := make([]*UserLog, 0)
	for rows.Next() {
		log := &UserLog{}
		err := rows.Scan(&log.ID, &log.UserUID, &log.Action, &log.Details, &log.CreatedAt)
		if err != nil {
			utils.LogWarn("USER_LOG", fmt.Sprintf("Failed to scan log: %v", err))
			continue
		}
		logs = append(logs, log)
	}

	return logs, total, nil
}

// DeleteByUserUID 删除用户的所有日志（账户删除时调用）
// 注意：根据隐私政策，用户日志保留6个月，此方法仅供特殊情况使用
func (r *UserLogRepository) DeleteByUserUID(ctx context.Context, userUID string) error {
	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, "DELETE FROM user_logs WHERE user_uid = $1", userUID)
	if err != nil {
		return utils.LogError("USER_LOG", "DeleteByUserUID", err, fmt.Sprintf("user_uid=%s", userUID))
	}

	utils.LogInfo("USER_LOG", fmt.Sprintf("Logs deleted: user_uid=%s", userUID))
	return nil
}

// DeleteExpiredLogs 删除超过6个月的过期日志
// 应通过定时任务定期调用（如每天一次）
func (r *UserLogRepository) DeleteExpiredLogs(ctx context.Context) (int64, error) {
	if r.pool == nil {
		return 0, errors.New("database not ready")
	}

	// 删除6个月前的日志
	result, err := r.pool.Exec(ctx,
		"DELETE FROM user_logs WHERE created_at < NOW() - INTERVAL '6 months'")
	if err != nil {
		return 0, utils.LogError("USER_LOG", "DeleteExpiredLogs", err)
	}

	count := result.RowsAffected()
	if count > 0 {
		utils.LogInfo("USER_LOG", fmt.Sprintf("Expired logs deleted: count=%d", count))
	}
	return count, nil
}
