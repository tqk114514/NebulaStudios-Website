package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidToken    = models.ErrInvalidToken
	ErrTokenExpired    = models.ErrTokenExpired
	ErrTokenUsed       = models.ErrTokenUsed
	ErrInvalidCode     = models.ErrInvalidCode
	ErrCodeExpired     = models.ErrCodeExpired
	ErrEmailMismatch   = models.ErrEmailMismatch
	ErrTypeMismatch    = models.ErrTypeMismatch
	ErrTooManyAttempts = models.ErrTooManyAttempts
	ErrCodeNotVerified = models.ErrCodeNotVerified
)

const (
	TokenTypeRegister       = "register"
	TokenTypeResetPassword  = "reset_password"
	TokenTypeChangePassword = "change_password"
	TokenTypeDeleteAccount  = "delete_account"

	tokenExpiry     = 5 * time.Minute
	maxCodeAttempts = 5
	tokenUsed       = 1
	codeVerified    = 1
)

// TokenResult Token 验证结果
type TokenResult struct {
	Code  string `json:"code"`
	Email string `json:"email"`
	Type  string `json:"type"`
}

// CodeResult 验证码验证结果
type CodeResult struct {
	Type            string `json:"type"`
	AlreadyVerified bool   `json:"alreadyVerified"`
}

// TokenService Token 服务
type TokenService struct {
	tokenRepo        *models.TokenRepository
	codeRepo         *models.CodeRepository
	sessionTokenRepo *models.SessionTokenRepository
	pool             *pgxpool.Pool
}

// NewTokenService 创建 Token 服务
func NewTokenService(pool *pgxpool.Pool) *TokenService {
	utils.LogInfo("TOKEN", "Token service initialized")
	return &TokenService{
		tokenRepo:        models.NewTokenRepository(pool),
		codeRepo:         models.NewCodeRepository(pool),
		sessionTokenRepo: models.NewSessionTokenRepository(pool),
		pool:             pool,
	}
}

// CreateToken 创建 Token
func (s *TokenService) CreateToken(ctx context.Context, email, tokenType string) (string, int64, error) {
	if email == "" {
		return "", 0, errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	normalizedType := strings.TrimSpace(tokenType)
	if normalizedType == "" {
		normalizedType = TokenTypeRegister
	}

	tokenStr, err := utils.GenerateSecureToken()
	if err != nil {
		return "", 0, utils.LogError("TOKEN", "GenerateSecureToken", err)
	}

	now := time.Now().UnixMilli()
	expireTime := now + int64(tokenExpiry.Milliseconds())

	token := &models.Token{
		Token:      tokenStr,
		TokenHash:  models.HashToken(tokenStr),
		Email:      normalizedEmail,
		Type:       normalizedType,
		CreatedAt:  now,
		ExpireTime: expireTime,
	}

	if err := s.tokenRepo.Create(ctx, token); err != nil {
		return "", 0, err
	}

	return tokenStr, expireTime, nil
}

// ValidateAndUseToken 验证并使用 Token
// 使用原子 UPDATE ... RETURNING 消除 SELECT → 检查 → UPDATE 之间的竞态条件
func (s *TokenService) ValidateAndUseToken(ctx context.Context, tokenStr string) (*TokenResult, error) {
	if tokenStr == "" {
		return nil, models.ErrInvalidToken
	}

	now := time.Now().UnixMilli()
	tokenHash := models.HashToken(tokenStr)

	token, err := s.tokenRepo.MarkUsedAndGet(ctx, tokenHash, now)
	if err != nil {
		return nil, err
	}
	if token == nil {
		existingToken, findErr := s.tokenRepo.FindByToken(ctx, tokenHash)
		if findErr != nil {
			if utils.IsDatabaseNotFound(findErr) {
				utils.LogDebug("TOKEN", "Token not found", utils.TruncateIdentifier(tokenHash))
				return nil, models.ErrInvalidToken
			}
			return nil, findErr
		}
		if existingToken.IsExpired() {
			s.tokenRepo.DeleteByToken(ctx, tokenHash)
			return nil, models.ErrTokenExpired
		}
		return nil, models.ErrTokenUsed
	}

	var codeStr string
	if token.Code == nil || *token.Code == "" {
		codeStr, err = utils.GenerateCode()
		if err != nil {
			return nil, utils.LogError("TOKEN", "GenerateCode", err)
		}

		s.tokenRepo.UpdateCode(ctx, tokenHash, codeStr)

		code := &models.Code{
			Code:       codeStr,
			Email:      token.Email,
			Type:       token.Type,
			CreatedAt:  now,
			ExpireTime: now + int64(tokenExpiry.Milliseconds()),
		}
		s.codeRepo.Create(ctx, code)
	} else {
		codeStr = *token.Code
	}

	utils.LogInfo("TOKEN", fmt.Sprintf("Token validated: email=%s, type=%s", token.Email, token.Type))

	return &TokenResult{
		Code:  codeStr,
		Email: token.Email,
		Type:  token.Type,
	}, nil
}

// VerifyCode 验证验证码
func (s *TokenService) VerifyCode(ctx context.Context, codeStr, email, expectedType string) (*CodeResult, error) {
	if codeStr == "" {
		return nil, models.ErrInvalidCode
	}
	if email == "" {
		return nil, errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	code, err := s.codeRepo.FindByCode(ctx, codeStr)
	if err != nil {
		if utils.IsDatabaseNotFound(err) {
			utils.LogDebug("TOKEN", "Code not found", codeStr)
			return nil, models.ErrInvalidCode
		}
		return nil, err
	}

	if code.Email != normalizedEmail {
		utils.LogWarn("TOKEN", fmt.Sprintf("Email mismatch: expected=%s, got=%s", code.Email, normalizedEmail))
		return nil, models.ErrEmailMismatch
	}

	if expectedType != "" && code.Type != expectedType {
		utils.LogWarn("TOKEN", fmt.Sprintf("Type mismatch: expected=%s, got=%s", expectedType, code.Type))
		return nil, models.ErrTypeMismatch
	}

	if code.IsExpired() {
		s.codeRepo.DeleteByCode(ctx, codeStr)
		return nil, models.ErrCodeExpired
	}

	if code.IsVerified() {
		return &CodeResult{Type: code.Type, AlreadyVerified: true}, nil
	}

	newAttempts := code.Attempts + 1
	if newAttempts > maxCodeAttempts {
		s.codeRepo.DeleteByCode(ctx, codeStr)
		utils.LogWarn("TOKEN", fmt.Sprintf("Too many attempts for code: email=%s", normalizedEmail))
		return nil, models.ErrTooManyAttempts
	}

	now := time.Now().UnixMilli()
	s.codeRepo.UpdateVerification(ctx, codeStr, newAttempts, now)

	utils.LogInfo("TOKEN", fmt.Sprintf("Code verified: email=%s, type=%s, attempts=%d", normalizedEmail, code.Type, newAttempts))

	return &CodeResult{Type: code.Type}, nil
}

// IsCodeVerified 检查验证码是否已验证
func (s *TokenService) IsCodeVerified(ctx context.Context, codeStr, email string) (bool, error) {
	if codeStr == "" {
		return false, models.ErrInvalidCode
	}
	if email == "" {
		return false, errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	code, err := s.codeRepo.FindByCode(ctx, codeStr)
	if err != nil {
		return false, models.ErrInvalidCode
	}

	if code.Email != normalizedEmail {
		return false, models.ErrEmailMismatch
	}

	if code.IsExpired() {
		s.codeRepo.DeleteByCode(ctx, codeStr)
		return false, models.ErrCodeExpired
	}

	if !code.IsVerified() {
		return false, models.ErrCodeNotVerified
	}

	return true, nil
}

// UseCode 使用验证码（删除）
func (s *TokenService) UseCode(ctx context.Context, codeStr, email string) error {
	if codeStr == "" {
		return models.ErrInvalidCode
	}
	if email == "" {
		return errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	code, err := s.codeRepo.FindByCode(ctx, codeStr)
	if err != nil {
		return models.ErrInvalidCode
	}

	if code.Email != normalizedEmail {
		return models.ErrEmailMismatch
	}
	if !code.IsVerified() {
		return models.ErrCodeNotVerified
	}

	s.codeRepo.DeleteByCode(ctx, codeStr)

	utils.LogInfo("TOKEN", fmt.Sprintf("Code used and removed: email=%s", normalizedEmail))
	return nil
}

// InvalidateCodeByEmail 使指定邮箱的验证码失效
func (s *TokenService) InvalidateCodeByEmail(ctx context.Context, email string, tokenType *string) error {
	if email == "" {
		utils.LogWarn("TOKEN", "InvalidateCodeByEmail called with empty email", "")
		return nil
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	return s.codeRepo.DeleteByEmail(ctx, normalizedEmail, tokenType)
}

// GetCodeExpiry 获取验证码过期时间
func (s *TokenService) GetCodeExpiry(ctx context.Context, codeStr, email string) (int64, error) {
	if codeStr == "" {
		return 0, models.ErrInvalidCode
	}
	if email == "" {
		return 0, errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	code, err := s.codeRepo.FindByCode(ctx, codeStr)
	if err != nil {
		return 0, models.ErrInvalidCode
	}

	if code.Email != normalizedEmail {
		return 0, models.ErrEmailMismatch
	}

	return code.ExpireTime, nil
}

// GetCodeExpiryByEmail 根据邮箱获取最新验证码的过期时间
func (s *TokenService) GetCodeExpiryByEmail(ctx context.Context, email string) (bool, int64, error) {
	if email == "" {
		return true, 0, errors.New("email is empty")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	now := time.Now().UnixMilli()
	expireTime, err := s.codeRepo.GetLatestExpiryByEmail(ctx, normalizedEmail, now)
	if err != nil {
		return true, 0, err
	}

	if expireTime == 0 {
		return true, 0, nil
	}

	return false, expireTime, nil
}

// CleanupExpired 清理过期数据
// 所有清理操作并行执行，提高效率
func (s *TokenService) CleanupExpired(ctx context.Context) {
	var wg sync.WaitGroup

	wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "TokenCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		now := time.Now().UnixMilli()
		count, err := s.tokenRepo.DeleteExpired(ctx, now)
		if err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup expired tokens", err)
			return
		}
		if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired tokens", count))
		}
	})

	wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "CodeCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		now := time.Now().UnixMilli()
		count, err := s.codeRepo.DeleteExpired(ctx, now)
		if err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup expired codes", err)
			return
		}
		if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired codes", count))
		}
	})

	wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "OAuthAuthCodeCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		repo := models.NewOAuthAuthCodeRepository(s.pool)
		if count, err := repo.DeleteExpired(ctx); err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup OAuth auth codes", err)
		} else if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired OAuth auth codes", count))
		}
	})

	wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "OAuthAccessTokenCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		repo := models.NewOAuthAccessTokenRepository(s.pool)
		if count, err := repo.DeleteExpired(ctx); err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup OAuth access tokens", err)
		} else if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired OAuth access tokens", count))
		}
	})

	wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "OAuthRefreshTokenCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		repo := models.NewOAuthRefreshTokenRepository(s.pool)
		if count, err := repo.DeleteExpired(ctx); err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup OAuth refresh tokens", err)
		} else if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired OAuth refresh tokens", count))
		}
	})

	wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("TOKEN", "SessionTokenCleanupPanic", fmt.Errorf("%v", r))
			}
		}()

		count, err := s.sessionTokenRepo.DeleteExpired(ctx)
		if err != nil {
			utils.LogWarn("TOKEN", "Failed to cleanup expired session tokens", err)
			return
		}
		if count > 0 {
			utils.LogInfo("TOKEN", fmt.Sprintf("Cleaned up %d expired session tokens", count))
		}
	})

	wg.Wait()
}

// GetTokenExpiry 获取 Token 过期时间配置
func (s *TokenService) GetTokenExpiry() time.Duration {
	return tokenExpiry
}
