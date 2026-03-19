/**
 * internal/models/email_whitelist.go
 * 邮箱白名单模型和数据访问层
 *
 * 功能：
 * - 邮箱域名白名单 CRUD 操作
 * - 域名查询（按域名、检查是否在白名单中）
 *
 * 依赖：
 * - PostgreSQL 数据库连接池
 */

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ====================  错误定义 ====================

var (
	// ErrEmailWhitelistNotFound 邮箱白名单条目未找到
	ErrEmailWhitelistNotFound = errors.New("email whitelist entry not found")
	// ErrEmailWhitelistDomainExists 域名已存在
	ErrEmailWhitelistDomainExists = errors.New("domain already exists in whitelist")
)

// ====================  数据结构 ====================

// EmailWhitelist 邮箱白名单
type EmailWhitelist struct {
	ID        int64     `json:"id"`
	Domain    string    `json:"domain"`
	SignupURL string    `json:"signup_url"`
	IsEnabled bool      `json:"is_enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EmailWhitelistRepository 邮箱白名单仓库
type EmailWhitelistRepository struct{}

// ====================  构造函数 ====================

// NewEmailWhitelistRepository 创建邮箱白名单仓库
func NewEmailWhitelistRepository() *EmailWhitelistRepository {
	return &EmailWhitelistRepository{}
}

// ====================  读取方法 ====================

// FindAll 获取所有白名单条目
func (r *EmailWhitelistRepository) FindAll(ctx context.Context) ([]*EmailWhitelist, error) {
	if pool == nil {
		return nil, ErrDBNotInitialized
	}

	rows, err := pool.Query(ctx, `
		SELECT id, domain, signup_url, is_enabled, created_at, updated_at
		FROM email_whitelist
		ORDER BY domain ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query email whitelist: %w", err)
	}
	defer rows.Close()

	var whitelist []*EmailWhitelist
	for rows.Next() {
		item := &EmailWhitelist{}
		err := rows.Scan(&item.ID, &item.Domain, &item.SignupURL, &item.IsEnabled, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan email whitelist: %w", err)
		}
		whitelist = append(whitelist, item)
	}

	return whitelist, nil
}

// FindAllPaginated 获取分页的白名单条目
func (r *EmailWhitelistRepository) FindAllPaginated(ctx context.Context, page int, pageSize int) ([]*EmailWhitelist, int64, error) {
	if pool == nil {
		return nil, 0, ErrDBNotInitialized
	}

	offset := (page - 1) * pageSize

	rows, err := pool.Query(ctx, `
		SELECT id, domain, signup_url, is_enabled, created_at, updated_at
		FROM email_whitelist
		ORDER BY domain ASC
		LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query email whitelist: %w", err)
	}
	defer rows.Close()

	var whitelist []*EmailWhitelist
	for rows.Next() {
		item := &EmailWhitelist{}
		err := rows.Scan(&item.ID, &item.Domain, &item.SignupURL, &item.IsEnabled, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan email whitelist: %w", err)
		}
		whitelist = append(whitelist, item)
	}

	var total int64
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM email_whitelist`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count email whitelist: %w", err)
	}

	return whitelist, total, nil
}

// FindByDomain 按域名查找
func (r *EmailWhitelistRepository) FindByDomain(ctx context.Context, domain string) (*EmailWhitelist, error) {
	if pool == nil {
		return nil, ErrDBNotInitialized
	}

	domain = strings.ToLower(strings.TrimSpace(domain))

	var item EmailWhitelist
	err := pool.QueryRow(ctx, `
		SELECT id, domain, signup_url, is_enabled, created_at, updated_at
		FROM email_whitelist
		WHERE domain = $1
	`, domain).Scan(&item.ID, &item.Domain, &item.SignupURL, &item.IsEnabled, &item.CreatedAt, &item.UpdatedAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEmailWhitelistNotFound
		}
		return nil, fmt.Errorf("failed to find email whitelist by domain: %w", err)
	}

	return &item, nil
}

// FindByID 按 ID 查找
func (r *EmailWhitelistRepository) FindByID(ctx context.Context, id int64) (*EmailWhitelist, error) {
	if pool == nil {
		return nil, ErrDBNotInitialized
	}

	var item EmailWhitelist
	err := pool.QueryRow(ctx, `
		SELECT id, domain, signup_url, is_enabled, created_at, updated_at
		FROM email_whitelist
		WHERE id = $1
	`, id).Scan(&item.ID, &item.Domain, &item.SignupURL, &item.IsEnabled, &item.CreatedAt, &item.UpdatedAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEmailWhitelistNotFound
		}
		return nil, fmt.Errorf("failed to find email whitelist by id: %w", err)
	}

	return &item, nil
}

// IsDomainAllowed 检查域名是否在白名单中且已启用
func (r *EmailWhitelistRepository) IsDomainAllowed(ctx context.Context, domain string) (bool, string, error) {
	if pool == nil {
		return false, "", ErrDBNotInitialized
	}

	domain = strings.ToLower(strings.TrimSpace(domain))

	var signupURL string
	var isEnabled bool
	err := pool.QueryRow(ctx, `
		SELECT signup_url, is_enabled
		FROM email_whitelist
		WHERE domain = $1
	`, domain).Scan(&signupURL, &isEnabled)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("failed to check domain allowance: %w", err)
	}

	return isEnabled, signupURL, nil
}

// ====================  写入方法 ====================

// Create 创建白名单条目
func (r *EmailWhitelistRepository) Create(ctx context.Context, domain, signupURL string) (*EmailWhitelist, error) {
	if pool == nil {
		return nil, ErrDBNotInitialized
	}

	domain = strings.ToLower(strings.TrimSpace(domain))
	signupURL = strings.TrimSpace(signupURL)

	if domain == "" {
		return nil, errors.New("domain is required")
	}
	if signupURL == "" {
		return nil, errors.New("signup_url is required")
	}

	var item EmailWhitelist
	err := pool.QueryRow(ctx, `
		INSERT INTO email_whitelist (domain, signup_url, is_enabled)
		VALUES ($1, $2, true)
		RETURNING id, domain, signup_url, is_enabled, created_at, updated_at
	`, domain, signupURL).Scan(&item.ID, &item.Domain, &item.SignupURL, &item.IsEnabled, &item.CreatedAt, &item.UpdatedAt)

	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, ErrEmailWhitelistDomainExists
		}
		return nil, fmt.Errorf("failed to create email whitelist: %w", err)
	}

	return &item, nil
}

// Update 更新白名单条目
func (r *EmailWhitelistRepository) Update(ctx context.Context, id int64, domain, signupURL string, isEnabled bool) (*EmailWhitelist, error) {
	if pool == nil {
		return nil, ErrDBNotInitialized
	}

	domain = strings.ToLower(strings.TrimSpace(domain))
	signupURL = strings.TrimSpace(signupURL)

	if id <= 0 {
		return nil, errors.New("invalid id")
	}
	if domain == "" {
		return nil, errors.New("domain is required")
	}
	if signupURL == "" {
		return nil, errors.New("signup_url is required")
	}

	var item EmailWhitelist
	err := pool.QueryRow(ctx, `
		UPDATE email_whitelist
		SET domain = $1, signup_url = $2, is_enabled = $3, updated_at = NOW()
		WHERE id = $4
		RETURNING id, domain, signup_url, is_enabled, created_at, updated_at
	`, domain, signupURL, isEnabled, id).Scan(&item.ID, &item.Domain, &item.SignupURL, &item.IsEnabled, &item.CreatedAt, &item.UpdatedAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEmailWhitelistNotFound
		}
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, ErrEmailWhitelistDomainExists
		}
		return nil, fmt.Errorf("failed to update email whitelist: %w", err)
	}

	return &item, nil
}

// Delete 删除白名单条目
func (r *EmailWhitelistRepository) Delete(ctx context.Context, id int64) error {
	if pool == nil {
		return ErrDBNotInitialized
	}

	if id <= 0 {
		return errors.New("invalid id")
	}

	result, err := pool.Exec(ctx, `DELETE FROM email_whitelist WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete email whitelist: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrEmailWhitelistNotFound
	}

	return nil
}

// SetEnabled 启用/禁用白名单条目
func (r *EmailWhitelistRepository) SetEnabled(ctx context.Context, id int64, isEnabled bool) error {
	if pool == nil {
		return ErrDBNotInitialized
	}

	if id <= 0 {
		return errors.New("invalid id")
	}

	result, err := pool.Exec(ctx, `
		UPDATE email_whitelist
		SET is_enabled = $1, updated_at = NOW()
		WHERE id = $2
	`, isEnabled, id)
	if err != nil {
		return fmt.Errorf("failed to set email whitelist enabled: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrEmailWhitelistNotFound
	}

	return nil
}
