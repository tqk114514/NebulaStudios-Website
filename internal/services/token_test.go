package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/utils"
)

// fakeTokenStore 本地验证令牌仓库 fake（services 测试不能 import testutil：会循环依赖）
type fakeTokenStore struct {
	created        []*models.Token
	createErr      error
	findResult     *models.Token
	findErr        error
	markUsedGet    *models.Token
	markUsedGetErr error
	updatedCodes   []string
	deleted        []string
}

func (f *fakeTokenStore) Create(_ context.Context, t *models.Token) error {
	f.created = append(f.created, t)
	return f.createErr
}
func (f *fakeTokenStore) FindByToken(context.Context, string) (*models.Token, error) {
	return f.findResult, f.findErr
}
func (f *fakeTokenStore) UpdateCode(_ context.Context, tokenHash, _ string) error {
	f.updatedCodes = append(f.updatedCodes, tokenHash)
	return nil
}
func (f *fakeTokenStore) MarkUsed(context.Context, string) error { return nil }
func (f *fakeTokenStore) MarkUsedAndGet(context.Context, string, int64) (*models.Token, error) {
	return f.markUsedGet, f.markUsedGetErr
}
func (f *fakeTokenStore) DeleteExpired(context.Context, int64) (int64, error) { return 0, nil }
func (f *fakeTokenStore) DeleteByToken(_ context.Context, tokenHash string) error {
	f.deleted = append(f.deleted, tokenHash)
	return nil
}

// fakeCodeStore 本地验证码仓库 fake
type fakeCodeStore struct {
	created       []*models.Code
	createErr     error
	findResult    *models.Code
	findErr       error
	updatedVerifs []string
	deletedCodes  []string
	deletedEmails []string
	latestExpiry  int64
}

func (f *fakeCodeStore) Create(_ context.Context, c *models.Code) error {
	f.created = append(f.created, c)
	return f.createErr
}
func (f *fakeCodeStore) FindByCode(context.Context, string) (*models.Code, error) {
	return f.findResult, f.findErr
}
func (f *fakeCodeStore) UpdateVerification(_ context.Context, codeStr string, _ int64) error {
	f.updatedVerifs = append(f.updatedVerifs, codeStr)
	return nil
}
func (f *fakeCodeStore) DeleteByCode(_ context.Context, codeStr string) error {
	f.deletedCodes = append(f.deletedCodes, codeStr)
	return nil
}
func (f *fakeCodeStore) DeleteByEmail(_ context.Context, email string, _ *string) error {
	f.deletedEmails = append(f.deletedEmails, email)
	return nil
}
func (f *fakeCodeStore) GetLatestExpiryByEmail(context.Context, string, int64) (int64, error) {
	return f.latestExpiry, nil
}
func (f *fakeCodeStore) DeleteExpired(context.Context, int64) (int64, error) { return 0, nil }

func newTokenServiceWithFakes(t *testing.T) (*TokenService, *fakeTokenStore, *fakeCodeStore) {
	t.Helper()
	tokenRepo := &fakeTokenStore{}
	codeRepo := &fakeCodeStore{}
	s := &TokenService{tokenRepo: tokenRepo, codeRepo: codeRepo}
	return s, tokenRepo, codeRepo
}

// ---------- CreateToken ----------

func TestCreateTokenEmptyEmail(t *testing.T) {
	s, _, _ := newTokenServiceWithFakes(t)
	if _, _, err := s.CreateToken(context.Background(), "", TokenTypeRegister); err == nil {
		t.Error("CreateToken(empty email) should error")
	}
}

func TestCreateTokenSuccess(t *testing.T) {
	s, repo, _ := newTokenServiceWithFakes(t)
	before := time.Now().UnixMilli()

	token, expireTime, err := s.CreateToken(context.Background(), "  A@B.C  ", TokenTypeRegister)
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}
	if token == "" {
		t.Error("CreateToken() returned empty token")
	}
	// 邮箱被 trim + 小写
	if len(repo.created) != 1 || repo.created[0].Email != "a@b.c" || repo.created[0].Type != TokenTypeRegister {
		t.Errorf("created token = %+v, want normalized email/type", repo.created)
	}
	if expireTime <= before {
		t.Errorf("expireTime = %d, want future", expireTime)
	}
}

// ---------- ValidateAndUseToken ----------

func TestValidateAndUseTokenGeneratesNewCode(t *testing.T) {
	s, tokenRepo, codeRepo := newTokenServiceWithFakes(t)
	tokenRepo.markUsedGet = &models.Token{
		TokenHash: "hash", Email: "a@b.c", Type: TokenTypeRegister,
		Code: nil, ExpireTime: time.Now().UnixMilli() + 60000,
	}

	result, err := s.ValidateAndUseToken(context.Background(), "raw-token")
	if err != nil {
		t.Fatalf("ValidateAndUseToken() error = %v", err)
	}
	if result.Email != "a@b.c" || result.Code == "" {
		t.Errorf("result = %+v, want email + generated code", result)
	}
	// 新 code 写回 token 表 + 新建 code 记录
	if len(tokenRepo.updatedCodes) != 1 {
		t.Errorf("UpdateCode not called, got %v", tokenRepo.updatedCodes)
	}
	if len(codeRepo.created) != 1 || codeRepo.created[0].Email != "a@b.c" {
		t.Errorf("code not created, got %v", codeRepo.created)
	}
}

func TestValidateAndUseTokenExistingCode(t *testing.T) {
	s, tokenRepo, codeRepo := newTokenServiceWithFakes(t)
	existing := "123456"
	tokenRepo.markUsedGet = &models.Token{
		TokenHash: "hash", Email: "a@b.c", Type: TokenTypeRegister,
		Code: &existing, ExpireTime: time.Now().UnixMilli() + 60000,
	}

	result, err := s.ValidateAndUseToken(context.Background(), "raw-token")
	if err != nil {
		t.Fatalf("ValidateAndUseToken() error = %v", err)
	}
	if result.Code != "123456" {
		t.Errorf("result.Code = %q, want existing code", result.Code)
	}
	// 已存在 code 时不重复创建
	if len(tokenRepo.updatedCodes) != 0 || len(codeRepo.created) != 0 {
		t.Errorf("existing code should not be recreated, updated=%v created=%v", tokenRepo.updatedCodes, codeRepo.created)
	}
}

func TestValidateAndUseTokenNotFound(t *testing.T) {
	s, repo, _ := newTokenServiceWithFakes(t)
	repo.markUsedGet = nil
	repo.findErr = &utils.DatabaseError{Operation: "FindByToken", NotFound: true}

	if _, err := s.ValidateAndUseToken(context.Background(), "raw-token"); !errors.Is(err, models.ErrInvalidToken) {
		t.Fatalf("ValidateAndUseToken() error = %v, want ErrInvalidToken", err)
	}
}

func TestValidateAndUseTokenExpiredFallback(t *testing.T) {
	s, repo, _ := newTokenServiceWithFakes(t)
	repo.markUsedGet = nil
	repo.findResult = &models.Token{
		TokenHash: "hash", Email: "a@b.c", Type: TokenTypeRegister,
		ExpireTime: time.Now().UnixMilli() - 1000,
	}

	if _, err := s.ValidateAndUseToken(context.Background(), "raw-token"); !errors.Is(err, models.ErrTokenExpired) {
		t.Fatalf("ValidateAndUseToken() error = %v, want ErrTokenExpired", err)
	}
	if len(repo.deleted) != 1 {
		t.Errorf("expired token should be deleted, got %v", repo.deleted)
	}
}

func TestValidateAndUseTokenAlreadyUsed(t *testing.T) {
	s, repo, _ := newTokenServiceWithFakes(t)
	repo.markUsedGet = nil
	repo.findResult = &models.Token{
		TokenHash: "hash", Email: "a@b.c", Type: TokenTypeRegister,
		ExpireTime: time.Now().UnixMilli() + 60000,
	}

	if _, err := s.ValidateAndUseToken(context.Background(), "raw-token"); !errors.Is(err, models.ErrTokenUsed) {
		t.Fatalf("ValidateAndUseToken() error = %v, want ErrTokenUsed", err)
	}
}

func TestValidateAndUseTokenEmpty(t *testing.T) {
	s, _, _ := newTokenServiceWithFakes(t)
	if _, err := s.ValidateAndUseToken(context.Background(), ""); !errors.Is(err, models.ErrInvalidToken) {
		t.Fatalf("ValidateAndUseToken(empty) error = %v, want ErrInvalidToken", err)
	}
}

// ---------- VerifyCode ----------

func unexpiredCode() *models.Code {
	return &models.Code{
		Code:       "123456",
		Email:      "a@b.c",
		Type:       TokenTypeRegister,
		ExpireTime: time.Now().UnixMilli() + 60000,
	}
}

func TestVerifyCodeSuccess(t *testing.T) {
	s, _, codeRepo := newTokenServiceWithFakes(t)
	codeRepo.findResult = unexpiredCode()

	result, err := s.VerifyCode(context.Background(), "123456", "A@B.C", TokenTypeRegister)
	if err != nil {
		t.Fatalf("VerifyCode() error = %v", err)
	}
	if result.Type != TokenTypeRegister || result.AlreadyVerified {
		t.Errorf("result = %+v, want register type not pre-verified", result)
	}
	if len(codeRepo.updatedVerifs) != 1 || codeRepo.updatedVerifs[0] != "123456" {
		t.Errorf("verification not marked, got %v", codeRepo.updatedVerifs)
	}
}

func TestVerifyCodeNotFound(t *testing.T) {
	s, _, codeRepo := newTokenServiceWithFakes(t)
	codeRepo.findErr = &utils.DatabaseError{Operation: "FindByCode", NotFound: true}

	if _, err := s.VerifyCode(context.Background(), "123456", "a@b.c", TokenTypeRegister); !errors.Is(err, models.ErrInvalidCode) {
		t.Fatalf("VerifyCode() error = %v, want ErrInvalidCode", err)
	}
}

func TestVerifyCodeEmailMismatch(t *testing.T) {
	s, _, codeRepo := newTokenServiceWithFakes(t)
	codeRepo.findResult = unexpiredCode()

	if _, err := s.VerifyCode(context.Background(), "123456", "other@b.c", TokenTypeRegister); !errors.Is(err, models.ErrEmailMismatch) {
		t.Fatalf("VerifyCode() error = %v, want ErrEmailMismatch", err)
	}
}

func TestVerifyCodeTypeMismatch(t *testing.T) {
	s, _, codeRepo := newTokenServiceWithFakes(t)
	codeRepo.findResult = unexpiredCode()

	if _, err := s.VerifyCode(context.Background(), "123456", "a@b.c", TokenTypeResetPassword); !errors.Is(err, models.ErrTypeMismatch) {
		t.Fatalf("VerifyCode() error = %v, want ErrTypeMismatch", err)
	}
}

func TestVerifyCodeExpired(t *testing.T) {
	s, _, codeRepo := newTokenServiceWithFakes(t)
	code := unexpiredCode()
	code.ExpireTime = time.Now().UnixMilli() - 1000
	codeRepo.findResult = code

	if _, err := s.VerifyCode(context.Background(), "123456", "a@b.c", TokenTypeRegister); !errors.Is(err, models.ErrCodeExpired) {
		t.Fatalf("VerifyCode() error = %v, want ErrCodeExpired", err)
	}
	if len(codeRepo.deletedCodes) != 1 {
		t.Errorf("expired code should be deleted, got %v", codeRepo.deletedCodes)
	}
}

func TestVerifyCodeAlreadyVerified(t *testing.T) {
	s, _, codeRepo := newTokenServiceWithFakes(t)
	code := unexpiredCode()
	code.Verified = 1
	codeRepo.findResult = code

	result, err := s.VerifyCode(context.Background(), "123456", "a@b.c", TokenTypeRegister)
	if err != nil {
		t.Fatalf("VerifyCode() error = %v", err)
	}
	if !result.AlreadyVerified {
		t.Error("AlreadyVerified = false for verified code")
	}
	// 已验证的 code 不重复标记
	if len(codeRepo.updatedVerifs) != 0 {
		t.Errorf("verified code should not be re-marked, got %v", codeRepo.updatedVerifs)
	}
}

func TestVerifyCodeEmpty(t *testing.T) {
	s, _, _ := newTokenServiceWithFakes(t)
	if _, err := s.VerifyCode(context.Background(), "", "a@b.c", TokenTypeRegister); !errors.Is(err, models.ErrInvalidCode) {
		t.Fatalf("VerifyCode(empty) error = %v, want ErrInvalidCode", err)
	}
	if _, err := s.VerifyCode(context.Background(), "123456", "", TokenTypeRegister); err == nil {
		t.Error("VerifyCode(empty email) should error")
	}
}
