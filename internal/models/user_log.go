/**
 * internal/models/user_log.go
 * 用户操作日志模型和数据访问层
 *
 * 功能：
 * - 用户账户操作日志记录
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
	// ErrUserLogDBNotReady 数据库未就绪
	ErrUserLogDBNotReady = errors.New("database not ready")
	// ErrUserLogInvalidData 无效的日志数据
	ErrUserLogInvalidData = errors.New("invalid user log data")
)

// ====================  常量定义 ====================

const (
	// UserActionRegister 用户注册
	UserActionRegister = "register"
	// UserActionChangePassword 修改密码
	UserActionChangePassword = "change_password"
	// UserActionChangeUsername 修改用户名
	UserActionChangeUsername = "change_username"
	// UserActionChangeAvatar 修改头像
	UserActionChangeAvatar = "change_avatar"
	// UserActionLinkMicrosoft 绑定微软账户
	UserActionLinkMicrosoft = "link_microsoft"
	// UserActionUnlinkMicrosoft 解绑微软账户
	UserActionUnlinkMicrosoft = "unlink_microsoft"
	// UserActionDeleteAccount 删除账户
	UserActionDeleteAccount = "delete_account"
	// UserActionBanned 被封禁
	UserActionBanned = "banned"
	// UserActionUnbanned 被解封
	UserActionUnbanned = "unbanned"
	// UserActionOAuthAuthorize OAuth 授权第三方应用
	UserActionOAuthAuthorize = "oauth_authorize"
	// UserActionOAuthRevoke OAuth 撤销第三方应用授权
	UserActionOAuthRevoke = "oauth_revoke"
)

// ====================  数据结构 ====================

// UserLog 用户操作日志
type UserLog struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"user_id"`
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
type UserLogRepository struct{}

// ====================  构造函数 ====================

// NewUserLogRepository 创建用户日志仓库
func NewUserLogRepository() *UserLogRepository {
	return &UserLogRepository{}
}


// ====================  写入方法 ====================

// Create 创建日志记录
func (r *UserLogRepository) Create(ctx context.Context, log *UserLog) error {
	if log == nil {
		return ErrUserLogInvalidData
	}
	if log.UserID <= 0 {
		return fmt.Errorf("%w: user_id is required", ErrUserLogInvalidData)
	}
	if log.Action == "" {
		return fmt.Errorf("%w: action is required", ErrUserLogInvalidData)
	}

	if err := r.checkDB(); err != nil {
		return err
	}

	err := pool.QueryRow(ctx, `
		INSERT INTO user_logs (user_id, action, details)
		VALUES ($1, $2, $3)
		RETURNING id, created_at
	`, log.UserID, log.Action, log.Details).Scan(&log.ID, &log.CreatedAt)

	if err != nil {
		utils.LogPrintf("[USER_LOG] ERROR: Failed to create log: error=%v", err)
		return fmt.Errorf("create user log failed: %w", err)
	}

	utils.LogPrintf("[USER_LOG] Log created: id=%d, user_id=%d, action=%s",
		log.ID, log.UserID, log.Action)
	return nil
}

// LogChangePassword 记录修改密码操作
func (r *UserLogRepository) LogChangePassword(ctx context.Context, userID int64) error {
	log := &UserLog{
		UserID: userID,
		Action: UserActionChangePassword,
	}
	return r.Create(ctx, log)
}

// LogRegister 记录用户注册操作
func (r *UserLogRepository) LogRegister(ctx context.Context, userID int64) error {
	log := &UserLog{
		UserID: userID,
		Action: UserActionRegister,
	}
	return r.Create(ctx, log)
}

// LogChangeUsername 记录修改用户名操作
func (r *UserLogRepository) LogChangeUsername(ctx context.Context, userID int64, oldUsername, newUsername string) error {
	details := ChangeUsernameDetails{
		OldUsername: oldUsername,
		NewUsername: newUsername,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserID:  userID,
		Action:  UserActionChangeUsername,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogChangeAvatar 记录修改头像操作
func (r *UserLogRepository) LogChangeAvatar(ctx context.Context, userID int64, oldURL, newURL string) error {
	details := ChangeAvatarDetails{
		OldAvatarURL: oldURL,
		NewAvatarURL: newURL,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserID:  userID,
		Action:  UserActionChangeAvatar,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogLinkMicrosoft 记录绑定微软账户操作
func (r *UserLogRepository) LogLinkMicrosoft(ctx context.Context, userID int64, microsoftID, microsoftName string) error {
	details := LinkMicrosoftDetails{
		MicrosoftID:   microsoftID,
		MicrosoftName: microsoftName,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserID:  userID,
		Action:  UserActionLinkMicrosoft,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogUnlinkMicrosoft 记录解绑微软账户操作
func (r *UserLogRepository) LogUnlinkMicrosoft(ctx context.Context, userID int64, microsoftID, microsoftName string) error {
	details := UnlinkMicrosoftDetails{
		MicrosoftID:   microsoftID,
		MicrosoftName: microsoftName,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserID:  userID,
		Action:  UserActionUnlinkMicrosoft,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogDeleteAccount 记录删除账户操作
func (r *UserLogRepository) LogDeleteAccount(ctx context.Context, userID int64) error {
	log := &UserLog{
		UserID: userID,
		Action: UserActionDeleteAccount,
	}
	return r.Create(ctx, log)
}

// LogBanned 记录被封禁
func (r *UserLogRepository) LogBanned(ctx context.Context, userID int64, reason string, unbanAt *time.Time) error {
	details := BannedDetails{
		Reason:  reason,
		UnbanAt: unbanAt,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserID:  userID,
		Action:  UserActionBanned,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogUnbanned 记录被解封
func (r *UserLogRepository) LogUnbanned(ctx context.Context, userID int64) error {
	log := &UserLog{
		UserID: userID,
		Action: UserActionUnbanned,
	}
	return r.Create(ctx, log)
}

// LogOAuthAuthorize 记录 OAuth 授权第三方应用
func (r *UserLogRepository) LogOAuthAuthorize(ctx context.Context, userID int64, clientID, clientName, scope string) error {
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
		UserID:  userID,
		Action:  UserActionOAuthAuthorize,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}

// LogOAuthRevoke 记录 OAuth 撤销第三方应用授权
func (r *UserLogRepository) LogOAuthRevoke(ctx context.Context, userID int64, clientID, clientName string) error {
	details := OAuthRevokeDetails{
		ClientID:   clientID,
		ClientName: clientName,
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal details failed: %w", err)
	}

	log := &UserLog{
		UserID:  userID,
		Action:  UserActionOAuthRevoke,
		Details: detailsJSON,
	}
	return r.Create(ctx, log)
}


// ====================  查询方法 ====================

// FindByUserID 查询用户的操作日志（分页）
func (r *UserLogRepository) FindByUserID(ctx context.Context, userID int64, page, pageSize int) ([]*UserLog, int64, error) {
	if err := r.checkDB(); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize

	// 查询总数
	var total int64
	err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM user_logs WHERE user_id = $1",
		userID,
	).Scan(&total)
	if err != nil {
		utils.LogPrintf("[USER_LOG] ERROR: Failed to count logs: error=%v", err)
		return nil, 0, fmt.Errorf("count user logs failed: %w", err)
	}

	// 查询日志列表
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, action, details, created_at
		FROM user_logs
		WHERE user_id = $1
		ORDER BY id DESC
		LIMIT $2 OFFSET $3
	`, userID, pageSize, offset)
	if err != nil {
		utils.LogPrintf("[USER_LOG] ERROR: Failed to query logs: error=%v", err)
		return nil, 0, fmt.Errorf("query user logs failed: %w", err)
	}
	defer rows.Close()

	logs := make([]*UserLog, 0)
	for rows.Next() {
		log := &UserLog{}
		err := rows.Scan(&log.ID, &log.UserID, &log.Action, &log.Details, &log.CreatedAt)
		if err != nil {
			utils.LogPrintf("[USER_LOG] ERROR: Failed to scan log: error=%v", err)
			continue
		}
		logs = append(logs, log)
	}

	return logs, total, nil
}

// DeleteByUserID 删除用户的所有日志（账户删除时调用）
// 注意：根据隐私政策，用户日志保留6个月，此方法仅供特殊情况使用
func (r *UserLogRepository) DeleteByUserID(ctx context.Context, userID int64) error {
	if err := r.checkDB(); err != nil {
		return err
	}

	_, err := pool.Exec(ctx, "DELETE FROM user_logs WHERE user_id = $1", userID)
	if err != nil {
		utils.LogPrintf("[USER_LOG] ERROR: Failed to delete logs: user_id=%d, error=%v", userID, err)
		return fmt.Errorf("delete user logs failed: %w", err)
	}

	utils.LogPrintf("[USER_LOG] Logs deleted: user_id=%d", userID)
	return nil
}

// DeleteExpiredLogs 删除超过6个月的过期日志
// 应通过定时任务定期调用（如每天一次）
func (r *UserLogRepository) DeleteExpiredLogs(ctx context.Context) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	// 删除6个月前的日志
	result, err := pool.Exec(ctx,
		"DELETE FROM user_logs WHERE created_at < NOW() - INTERVAL '6 months'")
	if err != nil {
		utils.LogPrintf("[USER_LOG] ERROR: Failed to delete expired logs: error=%v", err)
		return 0, fmt.Errorf("delete expired logs failed: %w", err)
	}

	count := result.RowsAffected()
	if count > 0 {
		utils.LogPrintf("[USER_LOG] Expired logs deleted: count=%d", count)
	}
	return count, nil
}

// ====================  私有方法 ====================

func (r *UserLogRepository) checkDB() error {
	if pool == nil {
		utils.LogPrintf("[USER_LOG] ERROR: Database pool is nil")
		return ErrUserLogDBNotReady
	}
	return nil
}
