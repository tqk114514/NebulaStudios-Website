package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidHash      = errors.New("invalid hash format")
	ErrRandomGeneration = errors.New("failed to generate random bytes")
	ErrEmptyPassword    = errors.New("password cannot be empty")
)

const codeChars = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz"

const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024
	argon2Threads = 1
	argon2KeyLen  = 32
	argon2SaltLen = 16
)

const (
	aesKeySize   = 32
	gcmNonceSize = 12
)

const (
	tokenByteSize = 8
	codeLength    = 6
	uidLength     = 16
)

const uidChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// GenerateSecureToken 生成 16 字符的安全 Token（8 字节 hex 编码）
// 使用 crypto/rand 生成密码学安全的随机数
func GenerateSecureToken() (string, error) {
	bytes := make([]byte, tokenByteSize)
	n, err := rand.Read(bytes)
	if err != nil {
		return "", LogError("CRYPTO", "GenerateSecureToken", err)
	}

	if n != tokenByteSize {
		err := fmt.Errorf("incomplete random read: got %d bytes, expected %d", n, tokenByteSize)
		return "", LogError("CRYPTO", "GenerateSecureToken", err)
	}

	token := hex.EncodeToString(bytes)
	LogDebug("CRYPTO", "Generated secure token", "length", len(token))
	return token, nil
}

// GenerateCode 生成 6 字符的验证码
// 使用 crypto/rand.Int 实现密码学安全的均匀随机选择
func GenerateCode() (string, error) {
	code := make([]byte, codeLength)
	charLen := big.NewInt(int64(len(codeChars)))

	for i := range codeLength {
		n, err := rand.Int(rand.Reader, charLen)
		if err != nil {
			return "", LogError("CRYPTO", "GenerateCode", err)
		}
		code[i] = codeChars[n.Int64()]
	}

	result := string(code)
	LogDebug("CRYPTO", "Generated verification code", "length", len(result))
	return result, nil
}

// GenerateUID 生成 16 位用户唯一标识符（Base62 编码）
// 字符集: A-Z, a-z, 0-9, 使用密码学安全的随机数
func GenerateUID() (string, error) {
	uid := make([]byte, uidLength)
	charsetLen := big.NewInt(int64(len(uidChars)))

	for i := range uid {
		idx, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", LogError("CRYPTO", "GenerateUID", err)
		}
		uid[i] = uidChars[idx.Int64()]
	}

	result := string(uid)
	LogDebug("CRYPTO", "Generated UID", "length", len(result))
	return result, nil
}

// HashPassword 使用 Argon2id 哈希密码
// 返回格式：$argon2id$v=19$m=65536,t=1,p=4$salt$hash
func HashPassword(password string) (string, error) {
	if password == "" {
		LogWarn("CRYPTO", "Attempted to hash empty password")
		return "", ErrEmptyPassword
	}

	salt := make([]byte, argon2SaltLen)
	n, err := rand.Read(salt)
	if err != nil {
		return "", LogError("CRYPTO", "HashPassword", err, "failed to generate salt")
	}
	if n != argon2SaltLen {
		err := fmt.Errorf("incomplete salt generation: got %d bytes, expected %d", n, argon2SaltLen)
		return "", LogError("CRYPTO", "HashPassword", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	result := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads, b64Salt, b64Hash)

	LogDebug("CRYPTO", "Password hashed successfully", "algorithm", "argon2id", "memory_kb", argon2Memory/1024)
	return result, nil
}

// VerifyPassword 验证密码是否匹配
// 使用常量时间比较防止时序攻击
func VerifyPassword(password, encodedHash string) (bool, error) {
	if password == "" {
		LogWarn("CRYPTO", "Attempted to verify empty password")
		return false, ErrEmptyPassword
	}

	if encodedHash == "" {
		LogWarn("CRYPTO", "Attempted to verify against empty hash")
		return false, ErrInvalidHash
	}

	if !strings.HasPrefix(encodedHash, "$argon2id$") {
		LogWarn("CRYPTO", "Invalid hash format: not argon2id")
		return false, ErrInvalidHash
	}

	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		LogWarn("CRYPTO", "Invalid hash format", "expected_parts", 6, "got", len(parts))
		return false, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		LogWarn("CRYPTO", "Failed to parse version", "error", err)
		return false, fmt.Errorf("%w: invalid version", ErrInvalidHash)
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		LogWarn("CRYPTO", "Failed to parse parameters", "error", err)
		return false, fmt.Errorf("%w: invalid parameters", ErrInvalidHash)
	}

	if memory == 0 || time == 0 || threads == 0 {
		LogWarn("CRYPTO", "Invalid hash parameters", "memory", memory, "time", time, "threads", threads)
		return false, fmt.Errorf("%w: zero parameters", ErrInvalidHash)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		LogWarn("CRYPTO", "Failed to decode salt", "error", err)
		return false, fmt.Errorf("%w: invalid salt encoding", ErrInvalidHash)
	}

	if len(salt) == 0 {
		LogWarn("CRYPTO", "Empty salt in hash")
		return false, fmt.Errorf("%w: empty salt", ErrInvalidHash)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		LogWarn("CRYPTO", "Failed to decode hash", "error", err)
		return false, fmt.Errorf("%w: invalid hash encoding", ErrInvalidHash)
	}

	if len(expectedHash) == 0 {
		LogWarn("CRYPTO", "Empty hash value")
		return false, fmt.Errorf("%w: empty hash", ErrInvalidHash)
	}

	hash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(expectedHash)))

	match := subtle.ConstantTimeCompare(hash, expectedHash) == 1

	LogDebug("CRYPTO", "Password verification result", "match", match)

	return match, nil
}

// HashToken 计算 token 的 SHA-256 哈希（hex 编码），用于 token 的数据库存储与查询
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// S256CodeChallenge 从 code_verifier 生成 S256 方式的 code_challenge
// RFC 7636: code_challenge = BASE64URL-ENCODE(SHA256(ASCII(code_verifier)))
func S256CodeChallenge(codeVerifier string) string {
	hash := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// VerifyPKCE 验证 code_verifier 是否匹配 code_challenge
// 本项目强制 PKCE（CreateAuthorizationCode 要求 codeChallenge 非空），
// 因此 codeChallenge 为空时 fail-closed 返回 false，避免 DB 篡改等绕过。
func VerifyPKCE(codeVerifier, codeChallenge, codeChallengeMethod string) bool {
	if codeChallenge == "" {
		return false
	}

	if codeVerifier == "" {
		return false
	}

	switch codeChallengeMethod {
	case "S256":
		expected := S256CodeChallenge(codeVerifier)
		return subtle.ConstantTimeCompare([]byte(expected), []byte(codeChallenge)) == 1
	case "plain":
		return subtle.ConstantTimeCompare([]byte(codeVerifier), []byte(codeChallenge)) == 1
	default:
		return false
	}
}

// ValidateCodeVerifier 验证 code_verifier 格式是否正确
// RFC 7636: 43-128 字符，只能包含 [A-Z]/[a-z]/[0-9]/[-._~]
func ValidateCodeVerifier(codeVerifier string) bool {
	if len(codeVerifier) < 43 || len(codeVerifier) > 128 {
		return false
	}

	for _, c := range codeVerifier {
		if !((c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~') {
			return false
		}
	}

	return true
}

// ValidateCodeChallenge 验证 code_challenge 格式是否正确
func ValidateCodeChallenge(codeChallenge, codeChallengeMethod string) bool {
	if codeChallenge == "" {
		return false
	}

	switch codeChallengeMethod {
	case "S256":
		if len(codeChallenge) != 43 {
			return false
		}
	case "plain":
		if len(codeChallenge) < 43 || len(codeChallenge) > 128 {
			return false
		}
	case "":
		return false
	default:
		return false
	}

	allowPlainChars := codeChallengeMethod == "plain"

	for _, c := range codeChallenge {
		if !((c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_' ||
			(allowPlainChars && (c == '.' || c == '~'))) {
			return false
		}
	}

	return true
}
