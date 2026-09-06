// TOTP 一次性恢复码的数据访问。明文恢复码仅在启用时返回一次，
// 数据库只保存 SHA-256 哈希；消费通过原子 UPDATE 保证单次使用。
package models

import (
	"context"
	"errors"

	"auth-system/internal/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TOTPRecoveryStore TOTP 恢复码数据访问接口
type TOTPRecoveryStore interface {
	// CreateBatch 覆盖式写入用户的恢复码哈希（先删除旧的未使用码）
	CreateBatch(ctx context.Context, userUID string, codeHashes []string) error
	// Consume 原子消费恢复码：未使用则标记已使用并返回归属 UID
	Consume(ctx context.Context, codeHash string) (userUID string, consumed bool, err error)
	// DeleteByUserUID 删除用户全部恢复码（关闭 TOTP 时调用）
	DeleteByUserUID(ctx context.Context, userUID string) error
}

// TOTPRecoveryRepository TOTP 恢复码仓库
type TOTPRecoveryRepository struct {
	pool *pgxpool.Pool
}

// NewTOTPRecoveryRepository 创建 TOTP 恢复码仓库
func NewTOTPRecoveryRepository(pool *pgxpool.Pool) *TOTPRecoveryRepository {
	return &TOTPRecoveryRepository{pool: pool}
}

// CreateBatch 覆盖式写入恢复码哈希（事务内先删后插）
func (r *TOTPRecoveryRepository) CreateBatch(ctx context.Context, userUID string, codeHashes []string) error {
	if userUID == "" {
		return errors.New("invalid user UID")
	}
	if len(codeHashes) == 0 {
		return errors.New("empty recovery code hashes")
	}
	if r.pool == nil {
		return errors.New("database not ready")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return utils.LogError("TOTP", "CreateBatch", err, "begin failed", userUID)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "DELETE FROM totp_recovery_codes WHERE user_uid = $1 AND used = false", userUID)
	if err != nil {
		return utils.LogError("TOTP", "CreateBatch", err, "delete old failed", userUID)
	}

	for _, hash := range codeHashes {
		if _, err := tx.Exec(ctx,
			"INSERT INTO totp_recovery_codes (user_uid, code_hash) VALUES ($1, $2)",
			userUID, hash,
		); err != nil {
			return utils.LogError("TOTP", "CreateBatch", err, "insert failed", userUID)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return utils.LogError("TOTP", "CreateBatch", err, "commit failed", userUID)
	}

	utils.LogInfo("TOTP", "Recovery codes stored", "user_uid", userUID, "count", len(codeHashes))
	return nil
}

// Consume 原子消费恢复码：仅当未使用时标记为已使用并返回归属 UID，
// 供调用方校验恢复码是否属于当前用户
func (r *TOTPRecoveryRepository) Consume(ctx context.Context, codeHash string) (string, bool, error) {
	if codeHash == "" {
		return "", false, errors.New("empty recovery code hash")
	}
	if r.pool == nil {
		return "", false, errors.New("database not ready")
	}

	var userUID string
	err := r.pool.QueryRow(ctx, `
		UPDATE totp_recovery_codes
		SET used = true, used_at = NOW()
		WHERE code_hash = $1 AND used = false
		RETURNING user_uid
	`, codeHash).Scan(&userUID)

	if err != nil {
		if utils.IsDatabaseNotFound(err) {
			return "", false, nil
		}
		return "", false, utils.LogError("TOTP", "Consume", err, "consume recovery code failed")
	}

	return userUID, true, nil
}

// DeleteByUserUID 删除用户全部恢复码
func (r *TOTPRecoveryRepository) DeleteByUserUID(ctx context.Context, userUID string) error {
	if userUID == "" {
		return errors.New("invalid user UID")
	}
	if r.pool == nil {
		return errors.New("database not ready")
	}

	_, err := r.pool.Exec(ctx, "DELETE FROM totp_recovery_codes WHERE user_uid = $1", userUID)
	if err != nil {
		return utils.LogError("TOTP", "DeleteByUserUID", err, userUID)
	}

	utils.LogInfo("TOTP", "Recovery codes deleted", "user_uid", userUID)
	return nil
}
