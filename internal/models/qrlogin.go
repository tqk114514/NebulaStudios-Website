/**
 * internal/models/qrlogin.go
 * 扫码登录模型和数据访问层
 *
 * 功能：
 * - 扫码登录 Token 数据结构定义
 * - Token CRUD 操作
 * - Token 查询和验证
 * - 事务支持（一次性消费）
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
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ====================  常量定义 ====================

const (
	// QRStatusPending 待扫描状态
	QRStatusPending = "pending"
	// QRStatusScanned 已扫描状态
	QRStatusScanned = "scanned"
	// QRStatusConfirmed 已确认状态
	QRStatusConfirmed = "confirmed"
	// QRStatusCancelled 已取消状态
	QRStatusCancelled = "cancelled"
)

// ====================  数据结构 ====================

// QRLoginToken 扫码登录 Token 模型
type QRLoginToken struct {
	Token          string         `json:"token"`
	Status         string         `json:"status"`
	UserID         sql.NullInt64  `json:"user_id"`
	PcIP           string         `json:"pc_ip"`
	PcUserAgent    string         `json:"pc_user_agent"`
	PcSessionToken sql.NullString `json:"pc_session_token"`
	CreatedAt      int64          `json:"created_at"`
	ScannedAt      sql.NullInt64  `json:"scanned_at"`
	ConfirmedAt    sql.NullInt64  `json:"confirmed_at"`
	ExpireTime     int64          `json:"expire_time"`
}

// QRLoginRepository 扫码登录仓库
type QRLoginRepository struct{}

// ====================  构造函数 ====================

// NewQRLoginRepository 创建扫码登录仓库
// 返回：
//   - *QRLoginRepository: 仓库实例
func NewQRLoginRepository() *QRLoginRepository {
	return &QRLoginRepository{}
}

// ====================  查询方法 ====================

// FindByToken 根据 Token 查找记录
// 参数：
//   - ctx: 上下文
//   - token: Token
//
// 返回：
//   - *QRLoginToken: Token 记录
//   - error: 错误信息
func (r *QRLoginRepository) FindByToken(ctx context.Context, token string) (*QRLoginToken, error) {
	if token == "" {
		return nil, errors.New("empty token")
	}

	if pool == nil {
		return nil, errors.New("database not ready")
	}

	qrToken := &QRLoginToken{}
	err := pool.QueryRow(ctx, `
		SELECT token, status, user_id, pc_ip, pc_user_agent, pc_session_token,
		       created_at, scanned_at, confirmed_at, expire_time
		FROM qr_login_tokens WHERE token = $1
	`, token).Scan(
		&qrToken.Token, &qrToken.Status, &qrToken.UserID, &qrToken.PcIP, &qrToken.PcUserAgent,
		&qrToken.PcSessionToken, &qrToken.CreatedAt, &qrToken.ScannedAt, &qrToken.ConfirmedAt, &qrToken.ExpireTime,
	)

	if err != nil {
		return nil, utils.HandleDatabaseError("QRLOGIN", "FindByToken", err, token)
	}

	return qrToken, nil
}

// ====================  写入方法 ====================

// Create 创建 Token
// 参数：
//   - ctx: 上下文
//   - qrToken: Token 对象
//
// 返回：
//   - error: 错误信息
func (r *QRLoginRepository) Create(ctx context.Context, qrToken *QRLoginToken) error {
	if qrToken == nil {
		return errors.New("qrToken object is nil")
	}
	if qrToken.Token == "" {
		return errors.New("empty token")
	}

	if pool == nil {
		return errors.New("database not ready")
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO qr_login_tokens (token, status, pc_ip, pc_user_agent, created_at, expire_time)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, qrToken.Token, qrToken.Status, qrToken.PcIP, qrToken.PcUserAgent, qrToken.CreatedAt, qrToken.ExpireTime)

	if err != nil {
		return utils.LogError("QRLOGIN", "Create", err, fmt.Sprintf("token=%s", qrToken.Token[:8]+"..."))
	}

	utils.LogInfo("QRLOGIN", fmt.Sprintf("Token created: ip=%s", qrToken.PcIP))
	return nil
}

// UpdateStatus 更新 Token 状态
// 参数：
//   - ctx: 上下文
//   - token: Token
//   - status: 新状态
//   - scannedAt: 扫描时间（可选）
//
// 返回：
//   - error: 错误信息
func (r *QRLoginRepository) UpdateStatus(ctx context.Context, token, status string, scannedAt *int64) error {
	if token == "" || status == "" {
		return errors.New("empty token or status")
	}

	if pool == nil {
		return errors.New("database not ready")
	}

	var err error
	if scannedAt != nil {
		_, err = pool.Exec(ctx, `
			UPDATE qr_login_tokens 
			SET status = $1, scanned_at = $2 
			WHERE token = $3
		`, status, *scannedAt, token)
	} else {
		_, err = pool.Exec(ctx, `
			UPDATE qr_login_tokens 
			SET status = $1 
			WHERE token = $2
		`, status, token)
	}

	if err != nil {
		return utils.LogError("QRLOGIN", "UpdateStatus", err, fmt.Sprintf("token=%s", token[:8]+"..."))
	}

	utils.LogInfo("QRLOGIN", fmt.Sprintf("Token status updated: status=%s", status))
	return nil
}

// UpdateStatusWithCondition 带条件原子更新 Token 状态
// 参数：
//   - ctx: 上下文
//   - token: Token
//   - fromStatus: 源状态（条件）
//   - toStatus: 目标状态
//   - scannedAt: 扫描时间（可选）
//
// 返回：
//   - bool: 是否更新成功（affected rows > 0）
//   - error: 错误信息
func (r *QRLoginRepository) UpdateStatusWithCondition(ctx context.Context, token, fromStatus, toStatus string, scannedAt *int64) (bool, error) {
	if token == "" || fromStatus == "" || toStatus == "" {
		return false, errors.New("invalid parameters")
	}

	if pool == nil {
		return false, errors.New("database not ready")
	}

	var commandTag pgconn.CommandTag
	var err error

	if scannedAt != nil {
		commandTag, err = pool.Exec(ctx, `
			UPDATE qr_login_tokens 
			SET status = $1, scanned_at = $2 
			WHERE token = $3 AND status = $4
		`, toStatus, *scannedAt, token, fromStatus)
	} else {
		commandTag, err = pool.Exec(ctx, `
			UPDATE qr_login_tokens 
			SET status = $1 
			WHERE token = $2 AND status = $3
		`, toStatus, token, fromStatus)
	}

	if err != nil {
		return false, utils.LogError("QRLOGIN", "UpdateStatusWithCondition", err, fmt.Sprintf("token=%s", token[:8]+"..."))
	}

	rowsAffected := commandTag.RowsAffected()
	success := rowsAffected > 0

	if success {
		utils.LogInfo("QRLOGIN", fmt.Sprintf("Token status atomically updated: %s -> %s", fromStatus, toStatus))
	}

	return success, nil
}

// ConfirmLogin 确认登录（更新状态、user_id、confirmed_at 和 pc_session_token）
// 参数：
//   - ctx: 上下文
//   - token: Token
//   - userID: 用户 ID
//   - pcSessionToken: PC 会话 Token
//
// 返回：
//   - error: 错误信息
func (r *QRLoginRepository) ConfirmLogin(ctx context.Context, token string, userID int64, pcSessionToken string) error {
	if token == "" || userID <= 0 || pcSessionToken == "" {
		return errors.New("invalid parameters")
	}

	if pool == nil {
		return errors.New("database not ready")
	}

	_, err := pool.Exec(ctx, `
		UPDATE qr_login_tokens 
		SET status = $1, user_id = $2, confirmed_at = $3, pc_session_token = $4
		WHERE token = $5
	`, QRStatusConfirmed, userID, time.Now().UnixMilli(), pcSessionToken, token)

	if err != nil {
		return utils.LogError("QRLOGIN", "ConfirmLogin", err, fmt.Sprintf("token=%s", token[:8]+"..."))
	}

	utils.LogInfo("QRLOGIN", fmt.Sprintf("Login confirmed: userID=%d", userID))
	return nil
}

// ConfirmLoginWithCondition 带条件原子确认登录
// 参数：
//   - ctx: 上下文
//   - token: Token
//   - userID: 用户 ID
//   - pcSessionToken: PC 会话 Token
//
// 返回：
//   - bool: 是否更新成功
//   - error: 错误信息
func (r *QRLoginRepository) ConfirmLoginWithCondition(ctx context.Context, token string, userID int64, pcSessionToken string) (bool, error) {
	if token == "" || userID <= 0 || pcSessionToken == "" {
		return false, errors.New("invalid parameters")
	}

	if pool == nil {
		return false, errors.New("database not ready")
	}

	commandTag, err := pool.Exec(ctx, `
		UPDATE qr_login_tokens 
		SET status = $1, user_id = $2, confirmed_at = $3, pc_session_token = $4
		WHERE token = $5 AND status = $6
	`, QRStatusConfirmed, userID, time.Now().UnixMilli(), pcSessionToken, token, QRStatusScanned)

	if err != nil {
		return false, utils.LogError("QRLOGIN", "ConfirmLoginWithCondition", err, fmt.Sprintf("token=%s", token[:8]+"..."))
	}

	rowsAffected := commandTag.RowsAffected()
	success := rowsAffected > 0

	if success {
		utils.LogInfo("QRLOGIN", fmt.Sprintf("Login atomically confirmed: userID=%d", userID))
	}

	return success, nil
}

// Delete 删除 Token
// 参数：
//   - ctx: 上下文
//   - token: Token
//
// 返回：
//   - error: 错误信息
func (r *QRLoginRepository) Delete(ctx context.Context, token string) error {
	if token == "" {
		return errors.New("empty token")
	}

	if pool == nil {
		return errors.New("database not ready")
	}

	result, err := pool.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token = $1", token)
	if err != nil {
		return utils.LogError("QRLOGIN", "Delete", err, fmt.Sprintf("token=%s", token[:8]+"..."))
	}

	if result.RowsAffected() == 0 {
		return utils.HandleDatabaseError("QRLOGIN", "Delete", errors.New("no rows affected"), token)
	}

	utils.LogInfo("QRLOGIN", fmt.Sprintf("Token deleted: token=%s", token[:8]+"..."))
	return nil
}

// ====================  事务方法 ====================

// ConsumeAndSetSession 验证并一次性消费 Token，同时验证 pc_session_token
// 参数：
//   - ctx: 上下文
//   - token: Token
//   - pcSessionToken: PC 会话 Token
//
// 返回：
//   - int64: 用户 ID
//   - error: 错误信息
func (r *QRLoginRepository) ConsumeAndSetSession(ctx context.Context, token, pcSessionToken string) (int64, error) {
	if token == "" || pcSessionToken == "" {
		return 0, errors.New("invalid parameters")
	}

	if pool == nil {
		return 0, errors.New("database not ready")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, utils.LogError("QRLOGIN", "ConsumeAndSetSession", err, "Failed to begin transaction")
	}
	defer tx.Rollback(ctx)

	var status string
	var expireTime int64
	var dbPcSessionToken sql.NullString
	var userID sql.NullInt64

	// 查询 Token 信息并加锁
	err = tx.QueryRow(ctx, `
		SELECT status, expire_time, pc_session_token, user_id 
		FROM qr_login_tokens 
		WHERE token = $1
		FOR UPDATE
	`, token).Scan(&status, &expireTime, &dbPcSessionToken, &userID)

	if err != nil {
		return 0, utils.HandleDatabaseError("QRLOGIN", "ConsumeAndSetSession", err, token)
	}

	// 检查是否过期
	if time.Now().UnixMilli() > expireTime {
		utils.LogWarn("QRLOGIN", "Token expired in ConsumeAndSetSession", "")
		_, _ = tx.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token = $1", token)
		_ = tx.Commit(ctx)
		return 0, errors.New("TOKEN_EXPIRED")
	}

	// 检查状态
	if status != QRStatusConfirmed {
		return 0, fmt.Errorf("invalid token status: %s", status)
	}

	// 验证 pc_session_token 是否匹配
	if !dbPcSessionToken.Valid || dbPcSessionToken.String != pcSessionToken {
		return 0, errors.New("INVALID_SESSION")
	}

	// 验证 user_id
	if !userID.Valid || userID.Int64 <= 0 {
		return 0, errors.New("INVALID_USER")
	}

	// 删除 Token（一次性消费）
	_, err = tx.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token = $1", token)
	if err != nil {
		utils.LogWarn("QRLOGIN", "Failed to delete token in ConsumeAndSetSession", "")
	}

	// 提交事务
	err = tx.Commit(ctx)
	if err != nil {
		return 0, utils.LogError("QRLOGIN", "ConsumeAndSetSession", err, "Failed to commit transaction")
	}

	return userID.Int64, nil
}
