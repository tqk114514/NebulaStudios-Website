/**
 * internal/models/oauth_client.go
 * OAuth 客户端模型和数据访问层
 *
 * 功能：
 * - OAuth 客户端数据结构定义
 * - 客户端 CRUD 操作
 * - 客户端查询（按 ID、ClientID）
 * - 数据验证和错误处理
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
	"strings"
	"time"
)

// ====================  错误定义 ====================

var (
	// ErrOAuthClientNotFound 客户端未找到
	ErrOAuthClientNotFound = errors.New("OAUTH_CLIENT_NOT_FOUND")
	// ErrOAuthClientIDExists ClientID 已存在
	ErrOAuthClientIDExists = errors.New("OAUTH_CLIENT_ID_EXISTS")
	// ErrOAuthClientDisabled 客户端已禁用
	ErrOAuthClientDisabled = errors.New("OAUTH_CLIENT_DISABLED")
	// ErrOAuthInvalidClientData 无效的客户端数据
	ErrOAuthInvalidClientData = errors.New("OAUTH_INVALID_CLIENT_DATA")
	// ErrOAuthClientRepoDBNotReady 数据库未就绪
	ErrOAuthClientRepoDBNotReady = errors.New("database not ready")
	// ErrOAuthClientRepoNilClient 客户端对象为空
	ErrOAuthClientRepoNilClient = errors.New("client object is nil")
	// ErrOAuthClientRepoInvalidID 无效的客户端 ID
	ErrOAuthClientRepoInvalidID = errors.New("invalid client ID")
	// ErrOAuthClientRepoEmptyIdentifier 空的查询标识符
	ErrOAuthClientRepoEmptyIdentifier = errors.New("empty identifier")
)

// ====================  常量定义 ====================

const (
	// oauthClientMaxUpdateFields 最大更新字段数
	oauthClientMaxUpdateFields = 5
)

// oauthClientAllowedUpdateFields 允许更新的字段白名单
var oauthClientAllowedUpdateFields = map[string]bool{
	"name":               true,
	"description":        true,
	"redirect_uri":       true,
	"is_enabled":         true,
	"client_secret_hash": true,
}

// ====================  数据结构 ====================

// OAuthClient OAuth 客户端模型
type OAuthClient struct {
	ID               int64     `json:"id"`
	ClientID         string    `json:"client_id"`
	ClientSecretHash string    `json:"-"` // 不序列化到 JSON
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	RedirectURI      string    `json:"redirect_uri"`
	IsEnabled        bool      `json:"is_enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// OAuthClientPublic 公开的客户端信息（用于列表展示）
type OAuthClientPublic struct {
	ID          int64     `json:"id"`
	ClientID    string    `json:"client_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	RedirectURI string    `json:"redirect_uri"`
	IsEnabled   bool      `json:"is_enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// OAuthClientRepository OAuth 客户端仓库
type OAuthClientRepository struct{}

// ====================  OAuthClient 方法 ====================

// ToPublic 转换为公开信息
// 返回：
//   - *OAuthClientPublic: 公开的客户端信息
func (c *OAuthClient) ToPublic() *OAuthClientPublic {
	if c == nil {
		return nil
	}

	return &OAuthClientPublic{
		ID:          c.ID,
		ClientID:    c.ClientID,
		Name:        c.Name,
		Description: c.Description,
		RedirectURI: c.RedirectURI,
		IsEnabled:   c.IsEnabled,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

// Validate 验证客户端数据
// 返回：
//   - error: 验证失败时返回错误
func (c *OAuthClient) Validate() error {
	if c == nil {
		return ErrOAuthClientRepoNilClient
	}
	if c.ClientID == "" {
		return fmt.Errorf("%w: client_id is empty", ErrOAuthInvalidClientData)
	}
	if c.Name == "" {
		return fmt.Errorf("%w: name is empty", ErrOAuthInvalidClientData)
	}
	if c.RedirectURI == "" {
		return fmt.Errorf("%w: redirect_uri is empty", ErrOAuthInvalidClientData)
	}
	return nil
}

// ====================  构造函数 ====================

// NewOAuthClientRepository 创建 OAuth 客户端仓库
// 返回：
//   - *OAuthClientRepository: 客户端仓库实例
func NewOAuthClientRepository() *OAuthClientRepository {
	return &OAuthClientRepository{}
}

// ====================  查询方法 ====================

// FindByID 根据 ID 查找客户端
// 参数：
//   - ctx: 上下文
//   - id: 客户端 ID（数据库主键）
//
// 返回：
//   - *OAuthClient: 客户端对象
//   - error: 错误信息
func (r *OAuthClientRepository) FindByID(ctx context.Context, id int64) (*OAuthClient, error) {
	// 参数验证
	if id <= 0 {
		return nil, ErrOAuthClientRepoInvalidID
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, err
	}

	client := &OAuthClient{}
	err := pool.QueryRow(ctx, `
		SELECT id, client_id, client_secret_hash, name, description, redirect_uri,
		       is_enabled, created_at, updated_at
		FROM oauth_clients WHERE id = $1
	`, id).Scan(
		&client.ID, &client.ClientID, &client.ClientSecretHash, &client.Name,
		&client.Description, &client.RedirectURI, &client.IsEnabled,
		&client.CreatedAt, &client.UpdatedAt,
	)

	if err != nil {
		return nil, r.handleQueryError(err, "FindByID", id)
	}

	return client, nil
}

// FindByClientID 根据 ClientID 查找客户端
// 参数：
//   - ctx: 上下文
//   - clientID: OAuth 客户端 ID
//
// 返回：
//   - *OAuthClient: 客户端对象
//   - error: 错误信息
func (r *OAuthClientRepository) FindByClientID(ctx context.Context, clientID string) (*OAuthClient, error) {
	// 参数验证
	if clientID == "" {
		return nil, ErrOAuthClientRepoEmptyIdentifier
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, err
	}

	client := &OAuthClient{}
	err := pool.QueryRow(ctx, `
		SELECT id, client_id, client_secret_hash, name, description, redirect_uri,
		       is_enabled, created_at, updated_at
		FROM oauth_clients WHERE client_id = $1
	`, clientID).Scan(
		&client.ID, &client.ClientID, &client.ClientSecretHash, &client.Name,
		&client.Description, &client.RedirectURI, &client.IsEnabled,
		&client.CreatedAt, &client.UpdatedAt,
	)

	if err != nil {
		return nil, r.handleQueryError(err, "FindByClientID", clientID)
	}

	return client, nil
}

// FindAll 查询客户端列表（分页、搜索）
// 参数：
//   - ctx: 上下文
//   - page: 页码（从 1 开始）
//   - pageSize: 每页数量
//   - search: 搜索关键词（匹配名称或描述）
//
// 返回：
//   - []*OAuthClient: 客户端列表
//   - int64: 总数
//   - error: 错误信息
func (r *OAuthClientRepository) FindAll(ctx context.Context, page, pageSize int, search string) ([]*OAuthClient, int64, error) {
	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return nil, 0, err
	}

	// 计算偏移量
	offset := (page - 1) * pageSize

	var total int64
	var rows interface{ Close() }
	var err error

	if search == "" {
		// 无搜索条件
		err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM oauth_clients").Scan(&total)
		if err != nil {
			utils.LogPrintf("[OAUTH_CLIENT] ERROR: Failed to count clients: error=%v", err)
			return nil, 0, fmt.Errorf("count clients failed: %w", err)
		}

		rows, err = pool.Query(ctx, `
			SELECT id, client_id, client_secret_hash, name, description, redirect_uri,
			       is_enabled, created_at, updated_at
			FROM oauth_clients
			ORDER BY id DESC
			LIMIT $1 OFFSET $2
		`, pageSize, offset)
	} else {
		// 有搜索条件
		searchPattern := "%" + search + "%"
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM oauth_clients 
			WHERE name ILIKE $1 OR description ILIKE $1 OR client_id ILIKE $1
		`, searchPattern).Scan(&total)
		if err != nil {
			utils.LogPrintf("[OAUTH_CLIENT] ERROR: Failed to count clients with search: error=%v", err)
			return nil, 0, fmt.Errorf("count clients failed: %w", err)
		}

		rows, err = pool.Query(ctx, `
			SELECT id, client_id, client_secret_hash, name, description, redirect_uri,
			       is_enabled, created_at, updated_at
			FROM oauth_clients
			WHERE name ILIKE $1 OR description ILIKE $1 OR client_id ILIKE $1
			ORDER BY id DESC
			LIMIT $2 OFFSET $3
		`, searchPattern, pageSize, offset)
	}

	if err != nil {
		utils.LogPrintf("[OAUTH_CLIENT] ERROR: Failed to query clients: error=%v", err)
		return nil, 0, fmt.Errorf("query clients failed: %w", err)
	}
	defer rows.Close()

	// 扫描结果
	clients := make([]*OAuthClient, 0)
	pgxRows := rows.(interface {
		Next() bool
		Scan(dest ...interface{}) error
	})

	for pgxRows.Next() {
		client := &OAuthClient{}
		err := pgxRows.Scan(
			&client.ID, &client.ClientID, &client.ClientSecretHash, &client.Name,
			&client.Description, &client.RedirectURI, &client.IsEnabled,
			&client.CreatedAt, &client.UpdatedAt,
		)
		if err != nil {
			utils.LogPrintf("[OAUTH_CLIENT] ERROR: Failed to scan client: error=%v", err)
			continue
		}
		clients = append(clients, client)
	}

	return clients, total, nil
}


// ====================  写入方法 ====================

// Create 创建客户端
// 参数：
//   - ctx: 上下文
//   - client: 客户端对象（ID、CreatedAt、UpdatedAt 会被自动填充）
//
// 返回：
//   - error: 错误信息
func (r *OAuthClientRepository) Create(ctx context.Context, client *OAuthClient) error {
	// 参数验证
	if client == nil {
		return ErrOAuthClientRepoNilClient
	}

	// 验证客户端数据
	if err := client.Validate(); err != nil {
		return err
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return err
	}

	// 执行插入
	err := pool.QueryRow(ctx, `
		INSERT INTO oauth_clients (client_id, client_secret_hash, name, description, redirect_uri, is_enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, client.ClientID, client.ClientSecretHash, client.Name, client.Description,
		client.RedirectURI, client.IsEnabled).Scan(
		&client.ID, &client.CreatedAt, &client.UpdatedAt,
	)

	if err != nil {
		return r.handleWriteError(err, "Create", client.ClientID)
	}

	utils.LogPrintf("[OAUTH_CLIENT] Client created: id=%d, client_id=%s, name=%s",
		client.ID, client.ClientID, client.Name)
	return nil
}

// Update 更新客户端
// 参数：
//   - ctx: 上下文
//   - id: 客户端 ID（数据库主键）
//   - updates: 要更新的字段映射
//
// 返回：
//   - error: 错误信息
func (r *OAuthClientRepository) Update(ctx context.Context, id int64, updates map[string]interface{}) error {
	// 参数验证
	if id <= 0 {
		return ErrOAuthClientRepoInvalidID
	}

	if len(updates) == 0 {
		utils.LogPrintf("[OAUTH_CLIENT] WARN: Update called with empty updates: id=%d", id)
		return nil
	}

	if len(updates) > oauthClientMaxUpdateFields {
		return fmt.Errorf("%w: too many update fields", ErrOAuthInvalidClientData)
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return err
	}

	// 构建动态 SQL（使用白名单验证字段）
	query, args, err := r.buildUpdateQuery(id, updates)
	if err != nil {
		return err
	}

	// 执行更新
	result, err := pool.Exec(ctx, query, args...)
	if err != nil {
		return r.handleWriteError(err, "Update", id)
	}

	if result.RowsAffected() == 0 {
		return ErrOAuthClientNotFound
	}

	utils.LogPrintf("[OAUTH_CLIENT] Client updated: id=%d, fields=%d", id, len(updates))
	return nil
}

// Delete 删除客户端
// 参数：
//   - ctx: 上下文
//   - id: 客户端 ID（数据库主键）
//
// 返回：
//   - error: 错误信息
func (r *OAuthClientRepository) Delete(ctx context.Context, id int64) error {
	// 参数验证
	if id <= 0 {
		return ErrOAuthClientRepoInvalidID
	}

	// 检查数据库连接
	if err := r.checkDB(); err != nil {
		return err
	}

	result, err := pool.Exec(ctx, "DELETE FROM oauth_clients WHERE id = $1", id)
	if err != nil {
		utils.LogPrintf("[OAUTH_CLIENT] ERROR: Failed to delete client: id=%d, error=%v", id, err)
		return fmt.Errorf("delete client failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrOAuthClientNotFound
	}

	utils.LogPrintf("[OAUTH_CLIENT] Client deleted: id=%d", id)
	return nil
}

// ====================  私有方法 ====================

// checkDB 检查数据库连接是否就绪
func (r *OAuthClientRepository) checkDB() error {
	if pool == nil {
		utils.LogPrintf("[OAUTH_CLIENT] ERROR: Database pool is nil")
		return ErrOAuthClientRepoDBNotReady
	}
	return nil
}

// handleQueryError 处理查询错误
// 参数：
//   - err: 原始错误
//   - operation: 操作名称
//   - identifier: 查询标识符
//
// 返回：
//   - error: 处理后的错误
func (r *OAuthClientRepository) handleQueryError(err error, operation string, identifier interface{}) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrOAuthClientNotFound
	}

	// pgx 使用不同的错误类型，检查错误消息
	if err.Error() == "no rows in result set" {
		return ErrOAuthClientNotFound
	}

	utils.LogPrintf("[OAUTH_CLIENT] ERROR: %s failed: identifier=%v, error=%v", operation, identifier, err)
	return fmt.Errorf("%s failed: %w", operation, err)
}

// handleWriteError 处理写入错误
// 参数：
//   - err: 原始错误
//   - operation: 操作名称
//   - identifier: 相关标识符
//
// 返回：
//   - error: 处理后的错误
func (r *OAuthClientRepository) handleWriteError(err error, operation string, identifier interface{}) error {
	errStr := err.Error()

	// 检查唯一约束冲突
	if strings.Contains(errStr, "oauth_clients_client_id_key") {
		return ErrOAuthClientIDExists
	}

	utils.LogPrintf("[OAUTH_CLIENT] ERROR: %s failed: identifier=%v, error=%v", operation, identifier, err)
	return fmt.Errorf("%s failed: %w", operation, err)
}

// buildUpdateQuery 构建更新 SQL 查询
// 参数：
//   - id: 客户端 ID
//   - updates: 要更新的字段映射
//
// 返回：
//   - string: SQL 查询
//   - []interface{}: 参数列表
//   - error: 错误信息
func (r *OAuthClientRepository) buildUpdateQuery(id int64, updates map[string]interface{}) (string, []interface{}, error) {
	var setClauses []string
	args := make([]interface{}, 0, len(updates)+1)
	argIndex := 1

	for key, value := range updates {
		// 验证字段是否在白名单中（防止 SQL 注入）
		if !oauthClientAllowedUpdateFields[key] {
			utils.LogPrintf("[OAUTH_CLIENT] WARN: Attempted to update disallowed field: %s", key)
			continue
		}

		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", key, argIndex))
		args = append(args, value)
		argIndex++
	}

	if len(setClauses) == 0 {
		return "", nil, fmt.Errorf("%w: no valid fields to update", ErrOAuthInvalidClientData)
	}

	// 添加 updated_at
	query := fmt.Sprintf(
		"UPDATE oauth_clients SET updated_at = CURRENT_TIMESTAMP, %s WHERE id = $%d",
		strings.Join(setClauses, ", "),
		argIndex,
	)
	args = append(args, id)

	return query, args, nil
}
