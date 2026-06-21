package models

import (
	"auth-system/internal/utils"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrSessionTokenNotFound     = errors.New("SESSION_TOKEN_NOT_FOUND")
	ErrSessionTokenExpired      = errors.New("SESSION_TOKEN_EXPIRED")
	ErrSessionTokenReused       = errors.New("SESSION_TOKEN_REUSED")
	ErrSessionTokenRevoked      = errors.New("SESSION_TOKEN_REVOKED")
	ErrSessionTokenRepoNotReady = errors.New("database not ready")
)

// SessionToken 刷新令牌
type SessionToken struct {
	ID        int64      `json:"id"`
	TokenHash string     `json:"-"`
	UserUID   string     `json:"user_uid"`
	FamilyID  string     `json:"family_id"`
	Banned    bool       `json:"banned"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	Used      bool       `json:"used"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

// IsExpired 检查是否已过期
func (t *SessionToken) IsExpired() bool {
	return t != nil && time.Now().After(t.ExpiresAt)
}

// SessionTokenRepository 刷新令牌仓库
type SessionTokenRepository struct {
	pool *pgxpool.Pool
}

// NewSessionTokenRepository 创建刷新令牌仓库
func NewSessionTokenRepository(pool *pgxpool.Pool) *SessionTokenRepository {
	return &SessionTokenRepository{pool: pool}
}

// Create 创建刷新令牌
func (r *SessionTokenRepository) Create(ctx context.Context, token *SessionToken) error {
	if token == nil {
		return fmt.Errorf("token object is nil")
	}

	if err := r.checkDB(); err != nil {
		return err
	}

	err := r.pool.QueryRow(ctx, `
		INSERT INTO session_tokens (token_hash, user_uid, family_id, banned, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, token.TokenHash, token.UserUID, token.FamilyID, token.Banned, token.ExpiresAt).Scan(
		&token.ID, &token.CreatedAt,
	)

	if err != nil {
		return utils.LogError("SESSION_TOKEN", "Create", err, fmt.Sprintf("user_uid=%s", token.UserUID))
	}

	utils.LogInfo("SESSION_TOKEN", fmt.Sprintf("Session token created: family_id=%s, user_uid=%s", token.FamilyID, token.UserUID))
	return nil
}

// FindByHash 根据 token hash 查找
func (r *SessionTokenRepository) FindByHash(ctx context.Context, tokenHash string) (*SessionToken, error) {
	if tokenHash == "" {
		return nil, ErrSessionTokenNotFound
	}

	if err := r.checkDB(); err != nil {
		return nil, err
	}

	token := &SessionToken{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, token_hash, user_uid, family_id, banned, expires_at, created_at, used, used_at
		FROM session_tokens WHERE token_hash = $1
	`, tokenHash).Scan(
		&token.ID, &token.TokenHash, &token.UserUID, &token.FamilyID, &token.Banned,
		&token.ExpiresAt, &token.CreatedAt, &token.Used, &token.UsedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrSessionTokenNotFound
		}
		return nil, utils.LogError("SESSION_TOKEN", "FindByHash", err, tokenHash)
	}

	return token, nil
}

// MarkUsed 标记为已使用
func (r *SessionTokenRepository) MarkUsed(ctx context.Context, id int64) error {
	if err := r.checkDB(); err != nil {
		return err
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE session_tokens SET used = TRUE, used_at = NOW() WHERE id = $1
	`, id)

	if err != nil {
		return utils.LogError("SESSION_TOKEN", "MarkUsed", err, fmt.Sprintf("id=%d", id))
	}

	return nil
}

// RevokeFamily 撤销整个 token 家族（检测到重放攻击时调用）
func (r *SessionTokenRepository) RevokeFamily(ctx context.Context, familyID string) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := r.pool.Exec(ctx, `
		DELETE FROM session_tokens WHERE family_id = $1
	`, familyID)

	if err != nil {
		return 0, utils.LogError("SESSION_TOKEN", "RevokeFamily", err, fmt.Sprintf("family_id=%s", familyID))
	}

	count := result.RowsAffected()
	utils.LogInfo("SESSION_TOKEN", fmt.Sprintf("Token family revoked: family_id=%s, count=%d", familyID, count))
	return count, nil
}

// RevokeUser 撤销用户的所有刷新令牌
func (r *SessionTokenRepository) RevokeUser(ctx context.Context, userUID string) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := r.pool.Exec(ctx, `
		DELETE FROM session_tokens WHERE user_uid = $1
	`, userUID)

	if err != nil {
		return 0, utils.LogError("SESSION_TOKEN", "RevokeUser", err, fmt.Sprintf("user_uid=%s", userUID))
	}

	count := result.RowsAffected()
	utils.LogInfo("SESSION_TOKEN", fmt.Sprintf("User tokens revoked: user_uid=%s, count=%d", userUID, count))
	return count, nil
}

// DeleteExpired 删除过期的刷新令牌
func (r *SessionTokenRepository) DeleteExpired(ctx context.Context) (int64, error) {
	if err := r.checkDB(); err != nil {
		return 0, err
	}

	result, err := r.pool.Exec(ctx, "DELETE FROM session_tokens WHERE expires_at < NOW()")
	if err != nil {
		return 0, utils.LogError("SESSION_TOKEN", "DeleteExpired", err)
	}

	count := result.RowsAffected()
	if count > 0 {
		utils.LogInfo("SESSION_TOKEN", fmt.Sprintf("Deleted %d expired session tokens", count))
	}
	return count, nil
}

func (r *SessionTokenRepository) checkDB() error {
	if r.pool == nil {
		utils.LogError("SESSION_TOKEN", "checkDB", ErrSessionTokenRepoNotReady)
		return ErrSessionTokenRepoNotReady
	}
	return nil
}
