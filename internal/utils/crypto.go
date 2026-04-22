/**
 * internal/utils/crypto.go
 * 加密工具模块
 *
 * 功能：
 * - 安全 Token 生成（64 字符 hex）
 * - 验证码生成（6 字符字母数字）
 * - AES-256-GCM 加密/解密（用于 QR 登录）
 * - Argon2id 密码哈希和验证
 *
 * 依赖：
 * - golang.org/x/crypto/argon2
 */

package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ====================  错误定义 ====================

var (
	// ErrInvalidKeyLength 密钥长度无效（必须为 32 字节）
	ErrInvalidKeyLength = errors.New("invalid key length, must be 32 bytes")

	// ErrDecryptionFailed 解密失败
	ErrDecryptionFailed = errors.New("decryption failed")

	// ErrInvalidHash 哈希格式无效
	ErrInvalidHash = errors.New("invalid hash format")

	// ErrRandomGeneration 随机数生成失败
	ErrRandomGeneration = errors.New("failed to generate random bytes")

	// ErrEmptyPassword 密码为空
	ErrEmptyPassword = errors.New("password cannot be empty")

	// ErrEmptyPlaintext 明文为空
	ErrEmptyPlaintext = errors.New("plaintext cannot be empty")

	// ErrEmptyCiphertext 密文为空
	ErrEmptyCiphertext = errors.New("ciphertext cannot be empty")

	// ErrInvalidCiphertextFormat 密文格式无效
	ErrInvalidCiphertextFormat = errors.New("invalid ciphertext format")

	// ErrCipherCreation AES cipher 创建失败
	ErrCipherCreation = errors.New("failed to create AES cipher")

	// ErrGCMCreation GCM 模式创建失败
	ErrGCMCreation = errors.New("failed to create GCM mode")
)

// ====================  常量定义 ====================

// 验证码字符集（排除容易混淆的字符：0, O, I, l）
const codeChars = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz"

// Argon2id 参数（平衡安全性和性能）
const (
	argon2Time    = 1         // 迭代次数
	argon2Memory  = 64 * 1024 // 64 MB
	argon2Threads = 2         // 并行度
	argon2KeyLen  = 32        // 输出长度
	argon2SaltLen = 16        // Salt 长度
)

// AES-GCM 参数
const (
	aesKeySize   = 32 // AES-256 密钥长度
	gcmNonceSize = 12 // GCM 推荐 nonce 长度
	gcmTagSize   = 16 // GCM 认证标签长度
)

// Token 参数
const (
	tokenByteSize = 8 // Token 字节长度（hex 编码后 16 字符）
	codeLength    = 6  // 验证码长度
	uidLength     = 16 // UID 长度
)

// UID 字符集（Base62: A-Z, a-z, 0-9）
const uidChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// ====================  Token 生成 ====================

// GenerateSecureToken 生成 16 字符的安全 Token（8 字节 hex 编码）
// 使用 crypto/rand 生成密码学安全的随机数
//
// 返回：
//   - string: 16 字符的十六进制 Token
//   - error: 随机数生成失败时返回错误
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
	LogDebug("CRYPTO", fmt.Sprintf("Generated secure token: length=%d", len(token)))
	return token, nil
}

// GenerateCode 生成 6 字符的验证码
// 使用 crypto/rand.Int 实现密码学安全的均匀随机选择
//
// 返回：
//   - string: 6 字符验证码
//   - error: 随机数生成失败时返回错误
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
	LogDebug("CRYPTO", fmt.Sprintf("Generated verification code: length=%d", len(result)))
	return result, nil
}

// GenerateUID 生成 16 位用户唯一标识符（Base62 编码）
// 字符集: A-Z, a-z, 0-9, 使用密码学安全的随机数
//
// 返回：
//   - string: 16 位 Base62 UID
//   - error: 随机数生成失败时返回错误
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
	LogDebug("CRYPTO", fmt.Sprintf("Generated UID: length=%d", len(result)))
	return result, nil
}

// ====================  密码哈希（Argon2id）====================

// HashPassword 使用 Argon2id 哈希密码
// 返回格式：$argon2id$v=19$m=65536,t=1,p=4$salt$hash
//
// 参数：
//   - password: 原始密码（不能为空）
//
// 返回：
//   - string: Argon2id 格式的哈希字符串
//   - error: 密码为空或随机数生成失败时返回错误
func HashPassword(password string) (string, error) {
	// 参数验证
	if password == "" {
		LogWarn("CRYPTO", "Attempted to hash empty password")
		return "", ErrEmptyPassword
	}

	// 生成随机 salt
	salt := make([]byte, argon2SaltLen)
	n, err := rand.Read(salt)
	if err != nil {
		return "", LogError("CRYPTO", "HashPassword", err, "failed to generate salt")
	}
	if n != argon2SaltLen {
		err := fmt.Errorf("incomplete salt generation: got %d bytes, expected %d", n, argon2SaltLen)
		return "", LogError("CRYPTO", "HashPassword", err)
	}

	// 生成哈希
	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	// 编码为标准格式
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	result := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads, b64Salt, b64Hash)

	LogDebug("CRYPTO", fmt.Sprintf("Password hashed successfully: algorithm=argon2id, memory=%dKB", argon2Memory/1024))
	return result, nil
}

// VerifyPassword 验证密码是否匹配
// 使用常量时间比较防止时序攻击
//
// 参数：
//   - password: 用户输入的密码
//   - encodedHash: 存储的 Argon2id 哈希
//
// 返回：
//   - bool: 密码是否匹配
//   - error: 哈希格式无效时返回错误
func VerifyPassword(password, encodedHash string) (bool, error) {
	// 参数验证
	if password == "" {
		LogWarn("CRYPTO", "Attempted to verify empty password")
		return false, ErrEmptyPassword
	}

	if encodedHash == "" {
		LogWarn("CRYPTO", "Attempted to verify against empty hash")
		return false, ErrInvalidHash
	}

	// 检查是否为 Argon2id 格式
	if !strings.HasPrefix(encodedHash, "$argon2id$") {
		LogWarn("CRYPTO", "Invalid hash format: not argon2id")
		return false, ErrInvalidHash
	}

	// 解析哈希格式：$argon2id$v=19$m=65536,t=1,p=4$salt$hash
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		LogWarn("CRYPTO", fmt.Sprintf("Invalid hash format: expected 6 parts, got %d", len(parts)))
		return false, ErrInvalidHash
	}

	// 解析版本号
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to parse version: %v", err))
		return false, fmt.Errorf("%w: invalid version", ErrInvalidHash)
	}

	// 解析参数
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to parse parameters: %v", err))
		return false, fmt.Errorf("%w: invalid parameters", ErrInvalidHash)
	}

	// 验证参数合理性
	if memory == 0 || time == 0 || threads == 0 {
		LogWarn("CRYPTO", fmt.Sprintf("Invalid hash parameters: memory=%d, time=%d, threads=%d", memory, time, threads))
		return false, fmt.Errorf("%w: zero parameters", ErrInvalidHash)
	}

	// 解码 salt
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to decode salt: %v", err))
		return false, fmt.Errorf("%w: invalid salt encoding", ErrInvalidHash)
	}

	if len(salt) == 0 {
		LogWarn("CRYPTO", "Empty salt in hash")
		return false, fmt.Errorf("%w: empty salt", ErrInvalidHash)
	}

	// 解码期望的哈希
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to decode hash: %v", err))
		return false, fmt.Errorf("%w: invalid hash encoding", ErrInvalidHash)
	}

	if len(expectedHash) == 0 {
		LogWarn("CRYPTO", "Empty hash value")
		return false, fmt.Errorf("%w: empty hash", ErrInvalidHash)
	}

	// 计算密码哈希
	hash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(expectedHash)))

	// 常量时间比较（防止时序攻击）
	match := subtle.ConstantTimeCompare(hash, expectedHash) == 1

	LogDebug("CRYPTO", fmt.Sprintf("Password verification result: %v", match))

	return match, nil
}

// ====================  AES-256-GCM 加密 ====================

// EncryptAESGCM 使用 AES-256-GCM 加密数据
// key 必须是 32 字节（256 位）
// 返回格式：iv.authTag.ciphertext（三段 base64，兼容 Node.js 版本）
//
// 参数：
//   - plaintext: 要加密的明文数据
//   - key: 32 字节的 AES-256 密钥
//
// 返回：
//   - string: 加密后的密文（base64 格式）
//   - error: 加密失败时返回错误
func EncryptAESGCM(plaintext []byte, key []byte) (string, error) {
	// 参数验证
	if len(plaintext) == 0 {
		LogWarn("CRYPTO", "Attempted to encrypt empty plaintext")
		return "", ErrEmptyPlaintext
	}

	if len(key) != aesKeySize {
		err := fmt.Errorf("invalid key length: got %d, expected %d", len(key), aesKeySize)
		return "", LogError("CRYPTO", "EncryptAESGCM", err)
	}

	// 创建 AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", LogError("CRYPTO", "EncryptAESGCM", err, "failed to create AES cipher")
	}

	// 创建 GCM 模式
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", LogError("CRYPTO", "EncryptAESGCM", err, "failed to create GCM mode")
	}

	// 生成随机 nonce (12 字节，GCM 推荐)
	nonce := make([]byte, gcm.NonceSize())
	n, err := io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return "", LogError("CRYPTO", "EncryptAESGCM", err, "failed to generate nonce")
	}
	if n != gcm.NonceSize() {
		err := fmt.Errorf("incomplete nonce generation")
		return "", LogError("CRYPTO", "EncryptAESGCM", err)
	}

	// 加密
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// 分离 ciphertext 和 authTag（authTag 是最后 16 字节）
	tagSize := gcm.Overhead()
	if len(ciphertext) < tagSize {
		err := fmt.Errorf("ciphertext too short")
		return "", LogError("CRYPTO", "EncryptAESGCM", err)
	}

	actualCiphertext := ciphertext[:len(ciphertext)-tagSize]
	authTag := ciphertext[len(ciphertext)-tagSize:]

	// 返回格式：iv.authTag.ciphertext（兼容 Node.js）
	result := base64.StdEncoding.EncodeToString(nonce) + "." +
		base64.StdEncoding.EncodeToString(authTag) + "." +
		base64.StdEncoding.EncodeToString(actualCiphertext)

	LogDebug("CRYPTO", fmt.Sprintf("Data encrypted successfully: plaintext_size=%d, ciphertext_size=%d", len(plaintext), len(result)))
	return result, nil
}

// DecryptAESGCM 使用 AES-256-GCM 解密数据
// key 必须是 32 字节（256 位）
// 输入格式：iv.authTag.ciphertext（三段 base64，兼容 Node.js 版本）
//
// 参数：
//   - ciphertextB64: base64 编码的密文
//   - key: 32 字节的 AES-256 密钥
//
// 返回：
//   - []byte: 解密后的明文
//   - error: 解密失败时返回错误
func DecryptAESGCM(ciphertextB64 string, key []byte) ([]byte, error) {
	// 参数验证
	if ciphertextB64 == "" {
		LogWarn("CRYPTO", "Attempted to decrypt empty ciphertext")
		return nil, ErrEmptyCiphertext
	}

	if len(key) != aesKeySize {
		err := fmt.Errorf("invalid key length: got %d, expected %d", len(key), aesKeySize)
		return nil, LogError("CRYPTO", "DecryptAESGCM", err)
	}

	// 解析三段格式
	parts := strings.Split(ciphertextB64, ".")
	if len(parts) != 3 {
		LogWarn("CRYPTO", fmt.Sprintf("Invalid ciphertext format: expected 3 parts, got %d", len(parts)))
		return nil, ErrInvalidCiphertextFormat
	}

	// 解码 nonce
	nonce, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to decode nonce: %v", err))
		return nil, fmt.Errorf("%w: invalid nonce", ErrDecryptionFailed)
	}

	if len(nonce) != gcmNonceSize {
		LogWarn("CRYPTO", fmt.Sprintf("Invalid nonce size: got %d, expected %d", len(nonce), gcmNonceSize))
		return nil, fmt.Errorf("%w: invalid nonce size", ErrDecryptionFailed)
	}

	// 解码 authTag
	authTag, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to decode authTag: %v", err))
		return nil, fmt.Errorf("%w: invalid authTag", ErrDecryptionFailed)
	}

	if len(authTag) != gcmTagSize {
		LogWarn("CRYPTO", fmt.Sprintf("Invalid authTag size: got %d, expected %d", len(authTag), gcmTagSize))
		return nil, fmt.Errorf("%w: invalid authTag size", ErrDecryptionFailed)
	}

	// 解码 ciphertext
	ciphertext, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to decode ciphertext: %v", err))
		return nil, fmt.Errorf("%w: invalid ciphertext", ErrDecryptionFailed)
	}

	// 创建 AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, LogError("CRYPTO", "DecryptAESGCM", err, "failed to create AES cipher")
	}

	// 创建 GCM 模式
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, LogError("CRYPTO", "DecryptAESGCM", err, "failed to create GCM mode")
	}

	// 重新组合 ciphertext + authTag（Go 的 GCM 期望这种格式）
	combined := append(ciphertext, authTag...)

	// 解密
	plaintext, err := gcm.Open(nil, nonce, combined, nil)
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Decryption failed: %v", err))
		return nil, ErrDecryptionFailed
	}

	LogDebug("CRYPTO", fmt.Sprintf("Data decrypted successfully: ciphertext_size=%d, plaintext_size=%d", len(ciphertextB64), len(plaintext)))
	return plaintext, nil
}

// ====================  密钥派生 ====================

// DeriveKeyFromString 使用 Argon2id 从字符串确定性派生 32 字节密钥
// 相同输入始终产生相同输出，确保服务器重启后密钥一致
// Salt 由 derivationSalt + keyStr 的 SHA-256 哈希确定性生成
//
// 参数：
//   - keyStr: 密钥字符串
//   - derivationSalt: 密钥派生 Salt（来自环境变量，不可为空）
//
// 返回：
//   - []byte: 32 字节的密钥
//   - error: 参数为空时返回错误
func DeriveKeyFromString(keyStr string, derivationSalt string) ([]byte, error) {
	if keyStr == "" {
		LogWarn("CRYPTO", "Attempted to derive key from empty string")
		return nil, errors.New("key string cannot be empty")
	}
	if derivationSalt == "" {
		LogWarn("CRYPTO", "Attempted to derive key with empty derivation salt")
		return nil, errors.New("derivation salt cannot be empty")
	}

	saltHash := sha256.Sum256([]byte(derivationSalt + ":" + keyStr))
	salt := saltHash[:argon2SaltLen]

	key := argon2.IDKey([]byte(keyStr), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	LogDebug("CRYPTO", fmt.Sprintf("Key derived using Argon2id (deterministic salt): salt=%s", hex.EncodeToString(salt)[:16]+"..."))
	return key, nil
}

// ====================  PKCE (Proof Key for Code Exchange) ====================

// S256CodeChallenge 从 code_verifier 生成 S256 方式的 code_challenge
// RFC 7636: code_challenge = BASE64URL-ENCODE(SHA256(ASCII(code_verifier)))
//
// 参数：
//   - codeVerifier: 43-128 字符的随机字符串
//
// 返回：
//   - string: code_challenge
func S256CodeChallenge(codeVerifier string) string {
	hash := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// VerifyPKCE 验证 code_verifier 是否匹配 code_challenge
//
// 参数：
//   - codeVerifier: 客户端提供的 code_verifier
//   - codeChallenge: 存储的 code_challenge
//   - codeChallengeMethod: 存储的 code_challenge_method ("S256" 或 "plain")
//
// 返回：
//   - bool: 是否验证通过
func VerifyPKCE(codeVerifier, codeChallenge, codeChallengeMethod string) bool {
	if codeChallenge == "" {
		return true
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
//
// 参数：
//   - codeVerifier: 要验证的 code_verifier
//
// 返回：
//   - bool: 是否有效
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
//
// 参数：
//   - codeChallenge: 要验证的 code_challenge
//   - codeChallengeMethod: code_challenge_method ("S256" 或 "plain")
//
// 返回：
//   - bool: 是否有效
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

	for _, c := range codeChallenge {
		if !((c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_') {
			return false
		}
	}

	return true
}
