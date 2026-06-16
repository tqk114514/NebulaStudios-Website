package models

import (
	"auth-system/internal/utils"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	QRStatusPending   = "pending"
	QRStatusScanned   = "scanned"
	QRStatusConfirmed = "confirmed"
	QRStatusCancelled = "cancelled"
)

// QRLoginToken 扫码登录 Token 模型
type QRLoginToken struct {
	Token          string         `json:"token"` // 明文 token，仅业务层使用
	TokenHash      string         `json:"-"`     // token 的 SHA-256 hash，写入 DB
	Status         string         `json:"status"`
	UserUID        sql.NullString `json:"user_uid"`
	PcIP           string         `json:"pc_ip"`
	PcUserAgent    string         `json:"pc_user_agent"`
	PcSessionToken sql.NullString `json:"pc_session_token"`
	CreatedAt      int64          `json:"created_at"`
	ScannedAt      sql.NullInt64  `json:"scanned_at"`
	ConfirmedAt    sql.NullInt64  `json:"confirmed_at"`
	ExpireTime     int64          `json:"expire_time"`
}

// QRLoginRepository 扫码登录仓库
type QRLoginRepository struct {
	pool *pgxpool.Pool
}

// NewQRLoginRepository 创建扫码登录仓库
func NewQRLoginRepository(pool *pgxpool.Pool) *QRLoginRepository {
	return &QRLoginRepository{pool: pool}
}

// FindByToken 根据 token hash 查找记录
func (r *QRLoginRepository) FindByToken(ctx context.Context, tokenHash string) (*QRLoginToken, error) {
	if tokenHash == "" {
		return nil, errors.New("empty token hash")
	}

	if r.pool == nil {
		return nil, errors.New("database not ready")
	}

	qrToken := &QRLoginToken{TokenHash: tokenHash}
	err := r.pool.QueryRow(ctx, `
		SELECT status, user_uid, pc_ip, pc_user_agent, pc_session_token,
		       created_at, scanned_at, confirmed_at, expire_time
		FROM qr_login_tokens WHERE token_hash = $1
	`, tokenHash).Scan(
		&qrToken.Status, &qrToken.UserUID, &qrToken.PcIP, &qrToken.PcUserAgent,
		&qrToken.PcSessionToken, &qrToken.CreatedAt, &qrToken.ScannedAt, &qrToken.ConfirmedAt, &qrToken.ExpireTime,
	)

	if err != nil {
		return nil, utils.HandleDatabaseError("QRLOGIN", "FindByToken", err, utils.TruncateIdentifier(tokenHash))
	}

	return qrToken, nil
}

// Create 创建 Token
func (r *QRLoginRepository) Create(ctx context.Context, qrToken *QRLoginToken) error {
	if qrToken == nil {
		return errors.New("qrToken object is nil")
	}
	if qrToken.TokenHash == "" {
		return errors.New("empty token hash")
	}

	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO qr_login_tokens (token_hash, status, pc_ip, pc_user_agent, created_at, expire_time)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, qrToken.TokenHash, qrToken.Status, qrToken.PcIP, qrToken.PcUserAgent, qrToken.CreatedAt, qrToken.ExpireTime)

	if err != nil {
		return utils.LogError("QRLOGIN", "Create", err, fmt.Sprintf("token_hash=%s", utils.TruncateIdentifier(qrToken.TokenHash)))
	}

	utils.LogInfo("QRLOGIN", fmt.Sprintf("Token created: ip=%s", qrToken.PcIP))
	return nil
}

// UpdateStatus 更新 Token 状态
func (r *QRLoginRepository) UpdateStatus(ctx context.Context, tokenHash, status string, scannedAt *int64) error {
	if tokenHash == "" || status == "" {
		return errors.New("empty token hash or status")
	}

	if r.pool == nil {
		return errors.New("database not ready")
	}

	var err error
	if scannedAt != nil {
		_, err = r.pool.Exec(ctx, `
			UPDATE qr_login_tokens
			SET status = $1, scanned_at = $2
			WHERE token_hash = $3
		`, status, *scannedAt, tokenHash)
	} else {
		_, err = r.pool.Exec(ctx, `
			UPDATE qr_login_tokens
			SET status = $1
			WHERE token_hash = $2
		`, status, tokenHash)
	}

	if err != nil {
		return utils.LogError("QRLOGIN", "UpdateStatus", err, fmt.Sprintf("token_hash=%s", utils.TruncateIdentifier(tokenHash)))
	}

	utils.LogInfo("QRLOGIN", fmt.Sprintf("Token status updated: status=%s", status))
	return nil
}

// UpdateStatusWithCondition 带条件原子更新 Token 状态
func (r *QRLoginRepository) UpdateStatusWithCondition(ctx context.Context, tokenHash, fromStatus, toStatus string, scannedAt *int64) (bool, error) {
	if tokenHash == "" || fromStatus == "" || toStatus == "" {
		return false, errors.New("invalid parameters")
	}

	if r.pool == nil {
		return false, errors.New("database not ready")
	}

	var commandTag pgconn.CommandTag
	var err error

	if scannedAt != nil {
		commandTag, err = r.pool.Exec(ctx, `
			UPDATE qr_login_tokens
			SET status = $1, scanned_at = $2
			WHERE token_hash = $3 AND status = $4
		`, toStatus, *scannedAt, tokenHash, fromStatus)
	} else {
		commandTag, err = r.pool.Exec(ctx, `
			UPDATE qr_login_tokens
			SET status = $1
			WHERE token_hash = $2 AND status = $3
		`, toStatus, tokenHash, fromStatus)
	}

	if err != nil {
		return false, utils.LogError("QRLOGIN", "UpdateStatusWithCondition", err, fmt.Sprintf("token_hash=%s", utils.TruncateIdentifier(tokenHash)))
	}

	rowsAffected := commandTag.RowsAffected()
	success := rowsAffected > 0

	if success {
		utils.LogInfo("QRLOGIN", fmt.Sprintf("Token status atomically updated: %s -> %s", fromStatus, toStatus))
	}

	return success, nil
}

// ConfirmLogin 确认登录（更新状态、user_uid、confirmed_at 和 pc_session_token）
func (r *QRLoginRepository) ConfirmLogin(ctx context.Context, tokenHash string, userUID string, pcSessionToken string) error {
	if tokenHash == "" || userUID == "" || pcSessionToken == "" {
		return errors.New("invalid parameters")
	}

	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE qr_login_tokens
		SET status = $1, user_uid = $2, confirmed_at = $3, pc_session_token = $4
		WHERE token_hash = $5
	`, QRStatusConfirmed, userUID, time.Now().UnixMilli(), pcSessionToken, tokenHash)

	if err != nil {
		return utils.LogError("QRLOGIN", "ConfirmLogin", err, fmt.Sprintf("token_hash=%s", utils.TruncateIdentifier(tokenHash)))
	}

	utils.LogInfo("QRLOGIN", fmt.Sprintf("Login confirmed: userUID=%s", userUID))
	return nil
}

// ConfirmLoginWithCondition 带条件原子确认登录
func (r *QRLoginRepository) ConfirmLoginWithCondition(ctx context.Context, tokenHash string, userUID string, pcSessionToken string) (bool, error) {
	if tokenHash == "" || userUID == "" || pcSessionToken == "" {
		return false, errors.New("invalid parameters")
	}

	if r.pool == nil {
		return false, errors.New("database not ready")
	}

	commandTag, err := r.pool.Exec(ctx, `
		UPDATE qr_login_tokens
		SET status = $1, user_uid = $2, confirmed_at = $3, pc_session_token = $4
		WHERE token_hash = $5 AND status = $6
	`, QRStatusConfirmed, userUID, time.Now().UnixMilli(), pcSessionToken, tokenHash, QRStatusScanned)

	if err != nil {
		return false, utils.LogError("QRLOGIN", "ConfirmLoginWithCondition", err, fmt.Sprintf("token_hash=%s", utils.TruncateIdentifier(tokenHash)))
	}

	rowsAffected := commandTag.RowsAffected()
	success := rowsAffected > 0

	if success {
		utils.LogInfo("QRLOGIN", fmt.Sprintf("Login atomically confirmed: userUID=%s", userUID))
	}

	return success, nil
}

// Delete 删除 Token
func (r *QRLoginRepository) Delete(ctx context.Context, tokenHash string) error {
	if tokenHash == "" {
		return errors.New("empty token hash")
	}

	if r.pool == nil {
		return errors.New("database not ready")
	}

	result, err := r.pool.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token_hash = $1", tokenHash)
	if err != nil {
		return utils.LogError("QRLOGIN", "Delete", err, fmt.Sprintf("token_hash=%s", utils.TruncateIdentifier(tokenHash)))
	}

	if result.RowsAffected() == 0 {
		return utils.HandleDatabaseError("QRLOGIN", "Delete", errors.New("no rows affected"), utils.TruncateIdentifier(tokenHash))
	}

	utils.LogInfo("QRLOGIN", fmt.Sprintf("Token deleted: token_hash=%s", utils.TruncateIdentifier(tokenHash)))
	return nil
}

// ConsumeAndSetSession 验证并一次性消费 Token，同时验证 pc_session_token
func (r *QRLoginRepository) ConsumeAndSetSession(ctx context.Context, tokenHash, pcSessionToken string) (string, error) {
	if tokenHash == "" || pcSessionToken == "" {
		return "", errors.New("invalid parameters")
	}

	if r.pool == nil {
		return "", errors.New("database not ready")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", utils.LogError("QRLOGIN", "ConsumeAndSetSession", err, "Failed to begin transaction")
	}
	defer tx.Rollback(ctx)

	var status string
	var expireTime int64
	var dbPcSessionToken sql.NullString
	var userUID sql.NullString

	// 查询 Token 信息并加锁
	err = tx.QueryRow(ctx, `
		SELECT status, expire_time, pc_session_token, user_uid
		FROM qr_login_tokens
		WHERE token_hash = $1
		FOR UPDATE
	`, tokenHash).Scan(&status, &expireTime, &dbPcSessionToken, &userUID)

	if err != nil {
		return "", utils.HandleDatabaseError("QRLOGIN", "ConsumeAndSetSession", err, utils.TruncateIdentifier(tokenHash))
	}

	// 检查是否过期
	if time.Now().UnixMilli() > expireTime {
		utils.LogWarn("QRLOGIN", "Token expired in ConsumeAndSetSession", "")
		_, _ = tx.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token_hash = $1", tokenHash)
		_ = tx.Commit(ctx)
		return "", errors.New("TOKEN_EXPIRED")
	}

	// 检查状态
	if status != QRStatusConfirmed {
		return "", fmt.Errorf("invalid token status: %s", status)
	}

	// 验证 pc_session_token 是否匹配
	if !dbPcSessionToken.Valid || dbPcSessionToken.String != pcSessionToken {
		return "", errors.New("INVALID_SESSION")
	}

	// 验证 user_uid
	if !userUID.Valid || userUID.String == "" {
		return "", errors.New("INVALID_USER")
	}

	// 删除 Token（一次性消费）
	_, err = tx.Exec(ctx, "DELETE FROM qr_login_tokens WHERE token_hash = $1", tokenHash)
	if err != nil {
		utils.LogWarn("QRLOGIN", "Failed to delete token in ConsumeAndSetSession", "")
	}

	// 提交事务
	err = tx.Commit(ctx)
	if err != nil {
		return "", utils.LogError("QRLOGIN", "ConsumeAndSetSession", err, "Failed to commit transaction")
	}

	return userUID.String, nil
}
