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
	"golang.org/x/crypto/hkdf"
)

var (
	ErrInvalidKeyLength        = errors.New("invalid key length, must be 32 bytes")
	ErrDecryptionFailed        = errors.New("decryption failed")
	ErrInvalidHash             = errors.New("invalid hash format")
	ErrRandomGeneration        = errors.New("failed to generate random bytes")
	ErrEmptyPassword           = errors.New("password cannot be empty")
	ErrEmptyPlaintext          = errors.New("plaintext cannot be empty")
	ErrEmptyCiphertext         = errors.New("ciphertext cannot be empty")
	ErrInvalidCiphertextFormat = errors.New("invalid ciphertext format")
	ErrCipherCreation          = errors.New("failed to create AES cipher")
	ErrGCMCreation             = errors.New("failed to create GCM mode")
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
	gcmTagSize   = 16
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
	LogDebug("CRYPTO", fmt.Sprintf("Generated secure token: length=%d", len(token)))
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
	LogDebug("CRYPTO", fmt.Sprintf("Generated verification code: length=%d", len(result)))
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
	LogDebug("CRYPTO", fmt.Sprintf("Generated UID: length=%d", len(result)))
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

	LogDebug("CRYPTO", fmt.Sprintf("Password hashed successfully: algorithm=argon2id, memory=%dKB", argon2Memory/1024))
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
		LogWarn("CRYPTO", fmt.Sprintf("Invalid hash format: expected 6 parts, got %d", len(parts)))
		return false, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to parse version: %v", err))
		return false, fmt.Errorf("%w: invalid version", ErrInvalidHash)
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to parse parameters: %v", err))
		return false, fmt.Errorf("%w: invalid parameters", ErrInvalidHash)
	}

	if memory == 0 || time == 0 || threads == 0 {
		LogWarn("CRYPTO", fmt.Sprintf("Invalid hash parameters: memory=%d, time=%d, threads=%d", memory, time, threads))
		return false, fmt.Errorf("%w: zero parameters", ErrInvalidHash)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to decode salt: %v", err))
		return false, fmt.Errorf("%w: invalid salt encoding", ErrInvalidHash)
	}

	if len(salt) == 0 {
		LogWarn("CRYPTO", "Empty salt in hash")
		return false, fmt.Errorf("%w: empty salt", ErrInvalidHash)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to decode hash: %v", err))
		return false, fmt.Errorf("%w: invalid hash encoding", ErrInvalidHash)
	}

	if len(expectedHash) == 0 {
		LogWarn("CRYPTO", "Empty hash value")
		return false, fmt.Errorf("%w: empty hash", ErrInvalidHash)
	}

	hash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(expectedHash)))

	match := subtle.ConstantTimeCompare(hash, expectedHash) == 1

	LogDebug("CRYPTO", fmt.Sprintf("Password verification result: %v", match))

	return match, nil
}

// EncryptAESGCM 使用 AES-256-GCM 加密数据
// key 必须是 32 字节（256 位）
// 返回格式：iv.authTag.ciphertext（三段 base64，兼容 Node.js 版本）
func EncryptAESGCM(plaintext []byte, key []byte) (string, error) {
	if len(plaintext) == 0 {
		LogWarn("CRYPTO", "Attempted to encrypt empty plaintext")
		return "", ErrEmptyPlaintext
	}

	if len(key) != aesKeySize {
		err := fmt.Errorf("invalid key length: got %d, expected %d", len(key), aesKeySize)
		return "", LogError("CRYPTO", "EncryptAESGCM", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", LogError("CRYPTO", "EncryptAESGCM", err, "failed to create AES cipher")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", LogError("CRYPTO", "EncryptAESGCM", err, "failed to create GCM mode")
	}

	nonce := make([]byte, gcm.NonceSize())
	n, err := io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return "", LogError("CRYPTO", "EncryptAESGCM", err, "failed to generate nonce")
	}
	if n != gcm.NonceSize() {
		err := fmt.Errorf("incomplete nonce generation")
		return "", LogError("CRYPTO", "EncryptAESGCM", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	tagSize := gcm.Overhead()
	if len(ciphertext) < tagSize {
		err := fmt.Errorf("ciphertext too short")
		return "", LogError("CRYPTO", "EncryptAESGCM", err)
	}

	actualCiphertext := ciphertext[:len(ciphertext)-tagSize]
	authTag := ciphertext[len(ciphertext)-tagSize:]

	result := base64.StdEncoding.EncodeToString(nonce) + "." +
		base64.StdEncoding.EncodeToString(authTag) + "." +
		base64.StdEncoding.EncodeToString(actualCiphertext)

	LogDebug("CRYPTO", fmt.Sprintf("Data encrypted successfully: plaintext_size=%d, ciphertext_size=%d", len(plaintext), len(result)))
	return result, nil
}

// DecryptAESGCM 使用 AES-256-GCM 解密数据
// key 必须是 32 字节（256 位）
// 输入格式：iv.authTag.ciphertext（三段 base64，兼容 Node.js 版本）
func DecryptAESGCM(ciphertextB64 string, key []byte) ([]byte, error) {
	if ciphertextB64 == "" {
		LogWarn("CRYPTO", "Attempted to decrypt empty ciphertext")
		return nil, ErrEmptyCiphertext
	}

	if len(key) != aesKeySize {
		err := fmt.Errorf("invalid key length: got %d, expected %d", len(key), aesKeySize)
		return nil, LogError("CRYPTO", "DecryptAESGCM", err)
	}

	parts := strings.Split(ciphertextB64, ".")
	if len(parts) != 3 {
		LogWarn("CRYPTO", fmt.Sprintf("Invalid ciphertext format: expected 3 parts, got %d", len(parts)))
		return nil, ErrInvalidCiphertextFormat
	}

	nonce, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to decode nonce: %v", err))
		return nil, fmt.Errorf("%w: invalid nonce", ErrDecryptionFailed)
	}

	if len(nonce) != gcmNonceSize {
		LogWarn("CRYPTO", fmt.Sprintf("Invalid nonce size: got %d, expected %d", len(nonce), gcmNonceSize))
		return nil, fmt.Errorf("%w: invalid nonce size", ErrDecryptionFailed)
	}

	authTag, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to decode authTag: %v", err))
		return nil, fmt.Errorf("%w: invalid authTag", ErrDecryptionFailed)
	}

	if len(authTag) != gcmTagSize {
		LogWarn("CRYPTO", fmt.Sprintf("Invalid authTag size: got %d, expected %d", len(authTag), gcmTagSize))
		return nil, fmt.Errorf("%w: invalid authTag size", ErrDecryptionFailed)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Failed to decode ciphertext: %v", err))
		return nil, fmt.Errorf("%w: invalid ciphertext", ErrDecryptionFailed)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, LogError("CRYPTO", "DecryptAESGCM", err, "failed to create AES cipher")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, LogError("CRYPTO", "DecryptAESGCM", err, "failed to create GCM mode")
	}

	combined := append(ciphertext, authTag...)

	plaintext, err := gcm.Open(nil, nonce, combined, nil)
	if err != nil {
		LogWarn("CRYPTO", fmt.Sprintf("Decryption failed: %v", err))
		return nil, ErrDecryptionFailed
	}

	LogDebug("CRYPTO", fmt.Sprintf("Data decrypted successfully: ciphertext_size=%d, plaintext_size=%d", len(ciphertextB64), len(plaintext)))
	return plaintext, nil
}

// DeriveKeyFromString 使用 HKDF-SHA256 从字符串确定性派生 32 字节密钥
// 相同输入始终产生相同输出，确保服务器重启后密钥一致
func DeriveKeyFromString(keyStr string, derivationSalt string) ([]byte, error) {
	if keyStr == "" {
		LogWarn("CRYPTO", "Attempted to derive key from empty string")
		return nil, errors.New("key string cannot be empty")
	}
	if derivationSalt == "" {
		LogWarn("CRYPTO", "Attempted to derive key with empty derivation salt")
		return nil, errors.New("derivation salt cannot be empty")
	}

	reader := hkdf.New(sha256.New, []byte(keyStr), []byte(derivationSalt), []byte("nebula-qrlogin-v1"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("hkdf key derivation failed: %w", err)
	}

	LogDebug("CRYPTO", fmt.Sprintf("Key derived using HKDF-SHA256: key_len=%d", len(key)))
	return key, nil
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
