// TOTP 两步验证服务：RFC 6238 算法实现（HMAC-SHA1、6 位、30 秒周期、±1 窗口）、
// 一次性恢复码管理、登录中转 pending token 与防爆破锁定。
// 算法部分为纯函数，用 RFC 6238 官方测试向量验证（见 totp_test.go）。
package services

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/utils"
)

const (
	// TOTP 参数（RFC 6238 推荐配置，与主流验证器 App 默认一致）
	totpPeriod   = 30
	totpDigits   = 6
	totpSkew     = 1 // 允许 ±1 个时间片（±30 秒）的时钟偏移
	totpHMACSize = sha1.Size

	// 密钥：20 随机字节 → base32 无填充 32 字符
	totpSecretBytes = 20

	// 登录中转 pending token
	pendingTOTPTokenTTL    = 5 * time.Minute
	pendingTOTPTokenBytes  = 32
	pendingTOTPCleanupTTL  = pendingTOTPTokenTTL
	pendingTOTPTokenLength = pendingTOTPTokenBytes * 2 // hex 编码长度

	// 防爆破：同一用户连续失败 5 次锁定 5 分钟（配合 IP 限流）
	totpMaxFailures     = 5
	totpLockoutDuration = 5 * time.Minute
	totpStateTTL        = 10 * time.Minute

	// 恢复码
	totpRecoveryCodeCount = 8
	totpRecoveryCodeHalf  = 4 // XXXX-XXXX
)

// totpIssuer otpauth URI 中的发行方标识（验证器 App 显示的账户归属）
const totpIssuer = "Nebula Studios"

// recoveryCodeAlphabet 去掉易混淆字符（0/O/1/I/L）的恢复码字母表
const recoveryCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

var (
	ErrTOTPCodeFormat = errors.New("totp code must be 6 digits")
	ErrTOTPSecret     = errors.New("invalid TOTP secret")
)

// TOTPService TOTP 两步验证服务
type TOTPService struct {
	recoveryRepo models.TOTPRecoveryStore

	mu           sync.Mutex
	pending      map[string]*totpPendingEntry // sha256(token) -> entry
	lastUsedStep map[string]int64             // uid -> 最近成功消费的时间片
	failures     map[string]*totpFailures     // uid -> 失败计数与锁定状态
}

type totpPendingEntry struct {
	uid       string
	expiresAt time.Time
}

type totpFailures struct {
	count       int
	lockedUntil time.Time
	lastSeen    time.Time
}

// 编译期确认接口实现
var _ TOTPManager = (*TOTPService)(nil)

// NewTOTPService 创建 TOTP 服务
func NewTOTPService(recoveryRepo models.TOTPRecoveryStore) (*TOTPService, error) {
	if recoveryRepo == nil {
		return nil, errors.New("recovery repo is required")
	}
	return &TOTPService{
		recoveryRepo: recoveryRepo,
		pending:      make(map[string]*totpPendingEntry),
		lastUsedStep: make(map[string]int64),
		failures:     make(map[string]*totpFailures),
	}, nil
}

// ---------- 密钥与算法 ----------

// GenerateSecret 生成 base32 无填充编码的 TOTP 密钥（32 字符）
func (s *TOTPService) GenerateSecret() string {
	buf := make([]byte, totpSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败属于系统级故障，直接 panic（与全局 GenerateUID 的处理策略一致）
		panic(fmt.Sprintf("generate totp secret failed: %v", err))
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
}

// OTPAuthURI 构建 otpauth:// URI
// 空格须编码为 %20（PathEscape）而非 +（QueryEscape）：部分验证器 App 不解码
// 查询参数中的 +，会把 issuer 字面显示为 "Nebula+Studios"
func (s *TOTPService) OTPAuthURI(email, secret string) string {
	label := url.PathEscape(totpIssuer + ":" + email)
	return fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s&algorithm=SHA1&digits=%d&period=%d",
		label, secret, url.PathEscape(totpIssuer), totpDigits, totpPeriod)
}

// totpCodeAt 计算指定时间片偏移下的 TOTP 码
func totpCodeAt(secret string, unixTime int64, stepOffset int64) (string, int64, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) == 0 {
		return "", 0, ErrTOTPSecret
	}

	counter := unixTime/totpPeriod + stepOffset
	var counterBuf [8]byte
	binary.BigEndian.PutUint64(counterBuf[:], uint64(counter))

	mac := hmac.New(sha1.New, key)
	mac.Write(counterBuf[:])
	sum := mac.Sum(nil)

	// RFC 4226 动态截断
	offset := sum[totpHMACSize-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	code := value % uint32(pow10(totpDigits))

	return fmt.Sprintf("%0*d", totpDigits, code), counter, nil
}

func pow10(n int) int {
	result := 1
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}

// normalizeTOTPCode 归一化用户输入（去空白，仅接受纯数字）
func normalizeTOTPCode(code string) (string, error) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits || !isAllDigits(code) {
		return "", ErrTOTPCodeFormat
	}
	return code, nil
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// verifyCodeAt 在 ±skew 窗口内校验验证码，返回匹配的时间片偏移
func verifyCodeAt(secret, code string, unixTime int64) (int64, bool, error) {
	normalized, err := normalizeTOTPCode(code)
	if err != nil {
		return 0, false, err
	}

	for offset := -int64(totpSkew); offset <= int64(totpSkew); offset++ {
		expected, _, err := totpCodeAt(secret, unixTime, offset)
		if err != nil {
			return 0, false, err
		}
		// 恒定时间比较，防时序侧信道
		if subtle.ConstantTimeCompare([]byte(expected), []byte(normalized)) == 1 {
			return offset, true, nil
		}
	}
	return 0, false, nil
}

// VerifyCode 校验验证码（无状态）
func (s *TOTPService) VerifyCode(secret, code string) bool {
	_, ok, err := verifyCodeAt(secret, code, time.Now().Unix())
	return err == nil && ok
}

// VerifyLoginCode 登录校验：匹配成功时要求该时间片未被此用户消费过（防重放）
func (s *TOTPService) VerifyLoginCode(secret, uid, code string) bool {
	unixNow := time.Now().Unix()
	normalized, err := normalizeTOTPCode(code)
	if err != nil {
		return false
	}

	for offset := -int64(totpSkew); offset <= int64(totpSkew); offset++ {
		expected, counter, err := totpCodeAt(secret, unixNow, offset)
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(normalized)) != 1 {
			continue
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		// 允许向后偏移的码（用户时钟稍慢），但拒绝已消费或过旧的时间片
		if last, seen := s.lastUsedStep[uid]; seen && counter <= last {
			return false
		}
		if s.lastUsedStep == nil {
			s.lastUsedStep = make(map[string]int64)
		}
		s.lastUsedStep[uid] = counter
		return true
	}
	return false
}

// ---------- 登录中转 pending token ----------

// CreatePendingToken 为已通过密码验证的用户签发中转 token
func (s *TOTPService) CreatePendingToken(uid string) (string, error) {
	buf := make([]byte, pendingTOTPTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate pending token failed: %w", err)
	}
	token := hex.EncodeToString(buf)
	hash := pendingTokenHash(token)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		s.pending = make(map[string]*totpPendingEntry)
	}
	// 单用户同时只保留一个未消费的中转 token，减少悬挂窗口
	for h, entry := range s.pending {
		if entry.uid == uid {
			delete(s.pending, h)
		}
	}
	s.pending[hash] = &totpPendingEntry{uid: uid, expiresAt: time.Now().Add(pendingTOTPTokenTTL)}

	utils.LogInfo("TOTP", "Pending login token created", "user_uid", uid)
	return token, nil
}

// ValidatePendingToken 校验中转 token（不消费：验证码打错时用户可直接重试，防爆破交给锁定机制）
func (s *TOTPService) ValidatePendingToken(token string) (string, bool) {
	if len(token) != pendingTOTPTokenLength {
		return "", false
	}
	entry, ok := s.pending[pendingTokenHash(token)]
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.uid, true
}

// ConsumePendingToken 消费中转 token（单次使用，过期无效）
func (s *TOTPService) ConsumePendingToken(token string) (string, bool) {
	if len(token) != pendingTOTPTokenLength {
		return "", false
	}
	hash := pendingTokenHash(token)

	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.pending[hash]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(s.pending, hash)
		}
		return "", false
	}
	delete(s.pending, hash)
	return entry.uid, true
}

func pendingTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---------- 恢复码 ----------

// GenerateRecoveryCodes 生成 8 个 XXXX-XXXX 格式的一次性恢复码
func (s *TOTPService) GenerateRecoveryCodes() []string {
	codes := make([]string, totpRecoveryCodeCount)
	for i := range codes {
		buf := make([]byte, totpRecoveryCodeHalf*2)
		if _, err := rand.Read(buf); err != nil {
			panic(fmt.Sprintf("generate recovery code failed: %v", err))
		}
		var b strings.Builder
		for j, v := range buf {
			if j == totpRecoveryCodeHalf {
				b.WriteByte('-')
			}
			b.WriteByte(recoveryCodeAlphabet[int(v)%len(recoveryCodeAlphabet)])
		}
		codes[i] = b.String()
	}
	return codes
}

// normalizeRecoveryCode 归一化恢复码输入（大写、去连字符与空白）
func normalizeRecoveryCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

// HashRecoveryCode 恢复码 SHA-256 哈希（入库与比对共用）
func HashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

// StoreRecoveryCodes 覆盖式存储恢复码哈希
func (s *TOTPService) StoreRecoveryCodes(ctx context.Context, uid string, codes []string) error {
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = HashRecoveryCode(c)
	}
	return s.recoveryRepo.CreateBatch(ctx, uid, hashes)
}

// ConsumeRecoveryCode 原子消费恢复码并校验归属用户
func (s *TOTPService) ConsumeRecoveryCode(ctx context.Context, uid, code string) (bool, error) {
	if strings.TrimSpace(code) == "" {
		return false, nil
	}
	ownerUID, consumed, err := s.recoveryRepo.Consume(ctx, HashRecoveryCode(code))
	if err != nil {
		return false, err
	}
	return consumed && ownerUID == uid, nil
}

// ClearRecoveryCodes 清除用户全部恢复码
func (s *TOTPService) ClearRecoveryCodes(ctx context.Context, uid string) error {
	return s.recoveryRepo.DeleteByUserUID(ctx, uid)
}

// ---------- 防爆破锁定 ----------

// IsLocked 检查用户是否处于失败锁定状态
func (s *TOTPService) IsLocked(uid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.failures[uid]
	if !ok {
		return false
	}
	return time.Now().Before(f.lockedUntil)
}

// RecordFailure 记录一次验证失败，连续失败达到阈值后锁定
func (s *TOTPService) RecordFailure(uid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	f, ok := s.failures[uid]
	if !ok {
		s.failures[uid] = &totpFailures{count: 1, lastSeen: now}
		return
	}
	f.count++
	f.lastSeen = now
	if f.count >= totpMaxFailures {
		f.lockedUntil = now.Add(totpLockoutDuration)
		utils.LogWarn("TOTP", "User locked out after repeated failures", "user_uid", uid, "duration", totpLockoutDuration.String())
	}
}

// ---------- 状态清理 ----------

// CleanupExpired 清理过期的中转 token、失败计数与重放记录（后台任务调用）
func (s *TOTPService) CleanupExpired() {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	pendingCleaned := 0
	for hash, entry := range s.pending {
		if now.After(entry.expiresAt) {
			delete(s.pending, hash)
			pendingCleaned++
		}
	}

	failureCleaned := 0
	for uid, f := range s.failures {
		// 失败状态随时间自然失效：锁定窗口（5 分钟）总是短于状态 TTL（10 分钟），
		// 因此按 lastSeen 清理即可，不会清除仍在生效的锁定
		if now.Sub(f.lastSeen) > totpStateTTL {
			delete(s.failures, uid)
			failureCleaned++
		}
	}

	currentStep := now.Unix() / totpPeriod
	replayCleaned := 0
	for uid, step := range s.lastUsedStep {
		// 只保留当前窗口附近的重放记录，过期条目不再有防护意义
		if currentStep-step > int64(totpSkew)+1 {
			delete(s.lastUsedStep, uid)
			replayCleaned++
		}
	}

	if pendingCleaned+failureCleaned+replayCleaned > 0 {
		utils.LogInfo("TOTP", "Expired state cleaned",
			"pending", pendingCleaned, "failures", failureCleaned, "replay", replayCleaned)
	}
}
