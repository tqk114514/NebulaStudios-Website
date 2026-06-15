package models

import (
	"context"
	"time"
)

// UserStore 用户数据访问接口
type UserStore interface {
	FindByID(ctx context.Context, id int64) (*User, error)
	FindByUID(ctx context.Context, uid string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindByEmailOrUsername(ctx context.Context, identifier string) (*User, error)
	FindByUsername(ctx context.Context, username string) (*User, error)
	FindByMicrosoftID(ctx context.Context, msID string) (*User, error)
	FindByGoogleID(ctx context.Context, googleID string) (*User, error)
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, uid string, updates map[string]any) error
	UpdatePassword(ctx context.Context, uid, plainPassword string) error
	Delete(ctx context.Context, uid string) error
	FindAll(ctx context.Context, page, pageSize int, search string) ([]*User, int64, error)
	GetStats(ctx context.Context) (*UserStats, error)
	Ban(ctx context.Context, userUID, adminUID string, reason string, unbanAt *time.Time) error
	Unban(ctx context.Context, userUID string) error
}

// UserLogStore 用户日志数据访问接口
type UserLogStore interface {
	Create(ctx context.Context, log *UserLog) error
	LogChangePassword(ctx context.Context, userUID string) error
	LogRegister(ctx context.Context, userUID string) error
	LogChangeUsername(ctx context.Context, userUID string, oldUsername, newUsername string) error
	LogChangeAvatar(ctx context.Context, userUID string, oldURL, newURL string) error
	LogLinkMicrosoft(ctx context.Context, userUID string, microsoftID, microsoftName string) error
	LogUnlinkMicrosoft(ctx context.Context, userUID string, microsoftID, microsoftName string) error
	LogLinkGoogle(ctx context.Context, userUID string, googleID, googleName string) error
	LogUnlinkGoogle(ctx context.Context, userUID string, googleID, googleName string) error
	LogDeleteAccount(ctx context.Context, userUID string) error
	LogBanned(ctx context.Context, userUID string, reason string, unbanAt *time.Time) error
	LogUnbanned(ctx context.Context, userUID string) error
	LogOAuthAuthorize(ctx context.Context, userUID string, clientID, clientName, scope string) error
	LogOAuthRevoke(ctx context.Context, userUID string, clientID, clientName string) error
	FindByUserUID(ctx context.Context, userUID string, page, pageSize int) ([]*UserLog, int64, error)
	DeleteByUserUID(ctx context.Context, userUID string) error
	DeleteExpiredLogs(ctx context.Context) (int64, error)
}

// QRLoginStore 扫码登录数据访问接口
type QRLoginStore interface {
	Create(ctx context.Context, qrToken *QRLoginToken) error
	FindByToken(ctx context.Context, token string) (*QRLoginToken, error)
	UpdateStatus(ctx context.Context, token, status string, scannedAt *int64) error
	UpdateStatusWithCondition(ctx context.Context, token, fromStatus, toStatus string, scannedAt *int64) (bool, error)
	ConfirmLogin(ctx context.Context, token string, userUID string, pcSessionToken string) error
	ConfirmLoginWithCondition(ctx context.Context, token string, userUID string, pcSessionToken string) (bool, error)
	Delete(ctx context.Context, token string) error
	ConsumeAndSetSession(ctx context.Context, token, pcSessionToken string) (string, error)
}

// EmailWhitelistStore 邮件白名单数据访问接口
type EmailWhitelistStore interface {
	FindAll(ctx context.Context) ([]*EmailWhitelist, error)
	FindAllPaginated(ctx context.Context, page int, pageSize int) ([]*EmailWhitelist, int64, error)
	FindByDomain(ctx context.Context, domain string) (*EmailWhitelist, error)
	FindByID(ctx context.Context, id int64) (*EmailWhitelist, error)
	IsDomainAllowed(ctx context.Context, domain string) (bool, string, error)
	Create(ctx context.Context, domain, signupURL, logoURL string) (*EmailWhitelist, error)
	Update(ctx context.Context, id int64, domain, signupURL, logoURL string, isEnabled bool) (*EmailWhitelist, error)
	Delete(ctx context.Context, id int64) error
	SetEnabled(ctx context.Context, id int64, isEnabled bool) error
	InitDefaultWhitelist(ctx context.Context, domains string) error
}

// AdminLogStore 管理员操作日志接口
type AdminLogStore interface {
	Create(ctx context.Context, log *AdminLog) error
	LogSetRole(ctx context.Context, adminUID, targetUID string, targetUsername string, oldRole, newRole int) error
	LogDeleteUser(ctx context.Context, adminUID, targetUID string, targetUsername, targetEmail string) error
	LogBanUser(ctx context.Context, adminUID, targetUID string, targetUsername, reason string, unbanAt *time.Time) error
	LogUnbanUser(ctx context.Context, adminUID, targetUID string, targetUsername string) error
	LogOAuthClientCreate(ctx context.Context, adminUID string, clientDBID int64, clientID, clientName string) error
	LogOAuthClientUpdate(ctx context.Context, adminUID string, clientDBID int64, clientID, clientName string) error
	LogOAuthClientDelete(ctx context.Context, adminUID string, clientDBID int64, clientID, clientName string) error
	LogOAuthClientRegenerateSecret(ctx context.Context, adminUID string, clientDBID int64, clientID, clientName string) error
	LogOAuthClientToggle(ctx context.Context, adminUID string, clientDBID int64, clientID, clientName string, enabled bool) error
	LogEmailWhitelistCreate(ctx context.Context, adminUID string, entry *EmailWhitelist) error
	LogEmailWhitelistUpdate(ctx context.Context, adminUID string, entry *EmailWhitelist) error
	LogEmailWhitelistDelete(ctx context.Context, adminUID string, id int64) error
	LogDataExport(ctx context.Context, adminUID string, usersCount, logsCount int) error
	LogDataImport(ctx context.Context, adminUID string, usersImported, logsImported int) error
	FindAll(ctx context.Context, page, pageSize int) ([]*AdminLogPublic, int64, error)
}

// DataExportImportStore 数据导入导出数据访问接口
type DataExportImportStore interface {
	QueryAllUsers(ctx context.Context) ([]map[string]any, error)
	QueryAllUserLogs(ctx context.Context) ([]map[string]any, error)
	ImportUsers(ctx context.Context, users []map[string]any) (int, error)
	ImportUserLogs(ctx context.Context, logs []map[string]any) (int, error)
	DeleteAllUsers(ctx context.Context) error
	DeleteAllUserLogs(ctx context.Context) error
}
