package models

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auth-system/internal/utils"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DataExportImportRepository 数据导入导出仓库，封装批量数据操作的 SQL 逻辑
type DataExportImportRepository struct {
	pool *pgxpool.Pool
}

// NewDataExportImportRepository 创建数据导入导出仓库
func NewDataExportImportRepository(pool *pgxpool.Pool) *DataExportImportRepository {
	return &DataExportImportRepository{pool: pool}
}

// QueryAllUsers 导出所有用户数据（包含密码哈希等完整字段）
func (r *DataExportImportRepository) QueryAllUsers(ctx context.Context) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT uid, username, email, password, avatar_url, microsoft_id,
		       microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       google_id, google_name, google_avatar_url,
		       is_banned, ban_reason, banned_at, banned_by, unban_at, role,
		       created_at, updated_at
		FROM users
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]map[string]any, 0)
	for rows.Next() {
		var (
			uid, username, email, password, avatarURL                           string
			microsoftID, microsoftName, microsoftAvatarURL, microsoftAvatarHash *string
			googleID, googleName, googleAvatarURL                               *string
			isBanned                                                            bool
			banReason, bannedBy                                                 *string
			bannedAt, unbanAt                                                   *time.Time
			role                                                                int
			createdAt, updatedAt                                                time.Time
		)

		if err := rows.Scan(
			&uid, &username, &email, &password, &avatarURL,
			&microsoftID, &microsoftName, &microsoftAvatarURL, &microsoftAvatarHash,
			&googleID, &googleName, &googleAvatarURL,
			&isBanned, &banReason, &bannedAt, &bannedBy, &unbanAt, &role,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}

		user := map[string]any{
			"uid":        uid,
			"username":   username,
			"email":      email,
			"password":   password,
			"avatar_url": avatarURL,
			"is_banned":  isBanned,
			"role":       role,
			"created_at": createdAt.Format(time.RFC3339),
			"updated_at": updatedAt.Format(time.RFC3339),
		}

		setNullableString(user, "microsoft_id", microsoftID)
		setNullableString(user, "microsoft_name", microsoftName)
		setNullableString(user, "microsoft_avatar_url", microsoftAvatarURL)
		setNullableString(user, "microsoft_avatar_hash", microsoftAvatarHash)
		setNullableString(user, "google_id", googleID)
		setNullableString(user, "google_name", googleName)
		setNullableString(user, "google_avatar_url", googleAvatarURL)
		setNullableString(user, "ban_reason", banReason)
		setNullableString(user, "banned_by", bannedBy)
		setNullableTime(user, "banned_at", bannedAt)
		setNullableTime(user, "unban_at", unbanAt)

		users = append(users, user)
	}

	return users, rows.Err()
}

// QueryAllUserLogs 导出所有用户日志数据
func (r *DataExportImportRepository) QueryAllUserLogs(ctx context.Context) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_uid, action, details, created_at
		FROM user_logs
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id        int64
			userUID   string
			action    string
			details   []byte
			createdAt time.Time
		)

		if err := rows.Scan(&id, &userUID, &action, &details, &createdAt); err != nil {
			return nil, err
		}

		log := map[string]any{
			"id":         id,
			"user_uid":   userUID,
			"action":     action,
			"created_at": createdAt.Format(time.RFC3339),
		}

		if len(details) > 0 {
			log["details"] = string(details)
		}

		logs = append(logs, log)
	}

	return logs, rows.Err()
}

const importUsersSQL = `
	INSERT INTO users (uid, username, email, password, avatar_url,
	                   microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
	                   google_id, google_name, google_avatar_url,
	                   is_banned, ban_reason, banned_at, banned_by, unban_at, role, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	ON CONFLICT (uid) DO UPDATE SET
		username = EXCLUDED.username,
		email = EXCLUDED.email,
		password = EXCLUDED.password,
		avatar_url = EXCLUDED.avatar_url,
		microsoft_id = EXCLUDED.microsoft_id,
		microsoft_name = EXCLUDED.microsoft_name,
		microsoft_avatar_url = EXCLUDED.microsoft_avatar_url,
		microsoft_avatar_hash = EXCLUDED.microsoft_avatar_hash,
		google_id = EXCLUDED.google_id,
		google_name = EXCLUDED.google_name,
		google_avatar_url = EXCLUDED.google_avatar_url,
		is_banned = EXCLUDED.is_banned,
		ban_reason = EXCLUDED.ban_reason,
		banned_at = EXCLUDED.banned_at,
		banned_by = EXCLUDED.banned_by,
		unban_at = EXCLUDED.unban_at,
		role = EXCLUDED.role,
		updated_at = EXCLUDED.updated_at
`

const importUserLogsSQL = `
	INSERT INTO user_logs (id, user_uid, action, details, created_at)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (id) DO NOTHING
`

// ImportUsersResult 导入用户结果统计
type ImportUsersResult struct {
	Imported        int // 成功导入数
	PasswordSkipped int // 因 password 不合法被跳过的数量（疑似篡改）
	RoleDowngraded  int // 因 role 不合法被降级为普通用户的数量（疑似篡改）
}

// ImportUsers 批量导入用户（ON CONFLICT upsert），使用 pgx.Batch 减少数据库往返
// 安全校验：role 必须为合法枚举值，password 必须为 Argon2id 哈希格式，防止篡改备份提权
func (r *DataExportImportRepository) ImportUsers(ctx context.Context, users []map[string]any) (ImportUsersResult, error) {
	batch := &pgx.Batch{}
	uids := make([]string, 0, len(users))
	result := ImportUsersResult{}

	for _, user := range users {
		uid, _ := user["uid"].(string)
		if uid == "" {
			continue
		}

		// 校验 role：仅允许合法枚举值，非法值降级为普通用户并计数
		role, _ := toInt(user["role"])
		switch role {
		case RoleUser, RoleAdmin, RoleSuperAdmin:
		default:
			utils.LogWarn("DATA-IMPORT", fmt.Sprintf("User %s has invalid role %d, downgraded to RoleUser", uid, role))
			role = RoleUser
			result.RoleDowngraded++
		}

		// 校验 password：必须是 Argon2id 哈希格式，防止篡改备份重置他人密码
		password := toString(user["password"])
		if !strings.HasPrefix(password, "$argon2") {
			utils.LogWarn("DATA-IMPORT", fmt.Sprintf("Skip importing user %s: invalid password hash format", uid))
			result.PasswordSkipped++
			continue
		}

		batch.Queue(importUsersSQL,
			uid,
			toString(user["username"]),
			toString(user["email"]),
			password,
			toString(user["avatar_url"]),
			toNullableString(user["microsoft_id"]),
			toNullableString(user["microsoft_name"]),
			toNullableString(user["microsoft_avatar_url"]),
			toNullableString(user["microsoft_avatar_hash"]),
			toNullableString(user["google_id"]),
			toNullableString(user["google_name"]),
			toNullableString(user["google_avatar_url"]),
			toBool(user["is_banned"]),
			toNullableString(user["ban_reason"]),
			toNullableTime(user["banned_at"]),
			toNullableString(user["banned_by"]),
			toNullableTime(user["unban_at"]),
			role,
			toTime(user["created_at"]),
			toTime(user["updated_at"]),
		)
		uids = append(uids, uid)
	}

	if len(uids) == 0 {
		return result, nil
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	for _, uid := range uids {
		if _, err := br.Exec(); err != nil {
			utils.LogWarn("DATA-IMPORT", fmt.Sprintf("Failed to import user %s: %v", uid, err))
		} else {
			result.Imported++
		}
	}

	return result, nil
}

// ImportUserLogs 批量导入用户日志（ON CONFLICT DO NOTHING），使用 pgx.Batch 减少数据库往返
func (r *DataExportImportRepository) ImportUserLogs(ctx context.Context, logs []map[string]any) (int, error) {
	batch := &pgx.Batch{}
	ids := make([]int64, 0, len(logs))

	for _, log := range logs {
		id, _ := toInt(log["id"])
		if id == 0 {
			continue
		}

		userUID, _ := log["user_uid"].(string)
		action, _ := log["action"].(string)
		details, _ := log["details"].(string)
		createdAt := toTime(log["created_at"])

		var detailsBytes []byte
		if details != "" {
			detailsBytes = []byte(details)
		}

		batch.Queue(importUserLogsSQL, id, userUID, action, detailsBytes, createdAt)
		ids = append(ids, int64(id))
	}

	if len(ids) == 0 {
		return 0, nil
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	imported := 0
	for _, id := range ids {
		if _, err := br.Exec(); err != nil {
			utils.LogWarn("DATA-IMPORT", fmt.Sprintf("Failed to import user log %d: %v", id, err))
		} else {
			imported++
		}
	}

	return imported, nil
}

// DeleteAllUsers 删除所有用户（数据导入 overwrite 模式使用）
func (r *DataExportImportRepository) DeleteAllUsers(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM users`)
	return err
}

// DeleteAllUserLogs 删除所有用户日志（数据导入 overwrite 模式使用）
func (r *DataExportImportRepository) DeleteAllUserLogs(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_logs`)
	return err
}

func setNullableString(m map[string]any, key string, v *string) {
	if v != nil {
		m[key] = *v
	} else {
		m[key] = nil
	}
}

func setNullableTime(m map[string]any, key string, t *time.Time) {
	if t != nil {
		m[key] = t.Format(time.RFC3339)
	} else {
		m[key] = nil
	}
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func toInt(v any) (int, error) {
	switch val := v.(type) {
	case float64:
		return int(val), nil
	case int:
		return val, nil
	case int64:
		return int(val), nil
	default:
		return 0, fmt.Errorf("cannot convert to int: %T", v)
	}
}

func toNullableString(v any) *string {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}

func toNullableTime(v any) *time.Time {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func toTime(v any) time.Time {
	s, _ := v.(string)
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now()
	}
	return t
}
