package models

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 政策类型常量
const (
	PolicyTypePrivacy = "privacy"
	PolicyTypeTerms   = "terms"
)

// UserConsent 用户政策同意记录
type UserConsent struct {
	ID            int64     `json:"id"`
	UserUID       string    `json:"user_uid"`
	PolicyType    string    `json:"policy_type"`
	PolicyVersion string    `json:"policy_version"`
	CreatedAt     time.Time `json:"created_at"`
}

// UserConsentRepository 用户同意记录数据访问层
type UserConsentRepository struct {
	pool *pgxpool.Pool
}

func NewUserConsentRepository(pool *pgxpool.Pool) *UserConsentRepository {
	return &UserConsentRepository{pool: pool}
}

// Create 创建同意记录
func (r *UserConsentRepository) Create(ctx context.Context, consent *UserConsent) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO user_consents (user_uid, policy_type, policy_version)
		VALUES ($1, $2, $3)
		RETURNING id, created_at
	`, consent.UserUID, consent.PolicyType, consent.PolicyVersion).Scan(&consent.ID, &consent.CreatedAt)
	return err
}

// LogConsent 记录用户对某政策版本的同意
func (r *UserConsentRepository) LogConsent(ctx context.Context, userUID, policyType, policyVersion string) error {
	consent := &UserConsent{
		UserUID:       userUID,
		PolicyType:    policyType,
		PolicyVersion: policyVersion,
	}
	return r.Create(ctx, consent)
}

// FindByUserUID 查询用户的所有同意记录
func (r *UserConsentRepository) FindByUserUID(ctx context.Context, userUID string) ([]*UserConsent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_uid, policy_type, policy_version, created_at
		FROM user_consents
		WHERE user_uid = $1
		ORDER BY created_at DESC
	`, userUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var consents []*UserConsent
	for rows.Next() {
		c := &UserConsent{}
		if err := rows.Scan(&c.ID, &c.UserUID, &c.PolicyType, &c.PolicyVersion, &c.CreatedAt); err != nil {
			return nil, err
		}
		consents = append(consents, c)
	}
	return consents, rows.Err()
}

// DeleteByUserUID 删除用户的所有同意记录（用户删除时调用，审计保留与 user_logs 相同）
func (r *UserConsentRepository) DeleteByUserUID(ctx context.Context, userUID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_consents WHERE user_uid = $1`, userUID)
	return err
}
