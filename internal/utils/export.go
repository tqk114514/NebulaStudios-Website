package utils

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

var (
	ErrExportDecryptionFailed = errors.New("decryption failed: file may be corrupted or tampered")
	ErrExportInvalidFormat    = errors.New("invalid file format")
	ErrExportInvalidSalt1     = errors.New("DATA_EXPORT_SALT is not configured or invalid")
	ErrExportInvalidSalt2     = errors.New("salt2 in file header is invalid")
)

const (
	exportHKDFInfo    = "nebula-export-v1"
	exportHeaderAlign = 256
	exportSalt2Size   = 32
)

// ExportHeader 导出文件的明文头
type ExportHeader struct {
	Version    int    `json:"version"`
	ExportedAt string `json:"exportedAt"`
	ExportedBy string `json:"exportedBy"`
	Salt2      string `json:"salt2"`
	UsersCount int    `json:"usersCount"`
	LogsCount  int    `json:"logsCount"`
}

// ExportPayload 导出文件的加密内容
type ExportPayload struct {
	Users    []map[string]any `json:"users"`
	UserLogs []map[string]any `json:"user_logs"`
}

// ParseExportSalt1 解析 Salt1（来自环境变量 DATA_EXPORT_SALT）
func ParseExportSalt1(b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, ErrExportInvalidSalt1
	}
	salt1, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExportInvalidSalt1, err)
	}
	if len(salt1) == 0 {
		return nil, ErrExportInvalidSalt1
	}
	return salt1, nil
}

// GenerateExportSalt2 生成随机 Salt2
func GenerateExportSalt2() []byte {
	salt2 := make([]byte, exportSalt2Size)
	if _, err := rand.Read(salt2); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return salt2
}

// exportDeriveKey 使用 HKDF-SHA256 从 Salt1 + Salt2 派生 AES-256 密钥
func exportDeriveKey(salt1, salt2 []byte) ([]byte, error) {
	reader := hkdf.New(sha256.New, salt1, salt2, []byte(exportHKDFInfo))
	key := make([]byte, aesKeySize)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("hkdf key derivation failed: %w", err)
	}
	return key, nil
}

// ExportEncrypt 加密导出数据
func ExportEncrypt(salt1, salt2 []byte, header *ExportHeader, payload *ExportPayload) ([]byte, error) {
	key, err := exportDeriveKey(salt1, salt2)
	if err != nil {
		return nil, err
	}

	header.Salt2 = base64.StdEncoding.EncodeToString(salt2)

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal header: %w", err)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	var gzipBuf bytes.Buffer
	gzipWriter := gzip.NewWriter(&gzipBuf)
	if _, err := gzipWriter.Write(payloadJSON); err != nil {
		gzipWriter.Close()
		return nil, fmt.Errorf("gzip compression failed: %w", err)
	}
	gzipWriter.Close()

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher creation failed: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes-gcm creation failed: %w", err)
	}

	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce generation failed: %w", err)
	}

	ciphertext := aesgcm.Seal(nonce, nonce, gzipBuf.Bytes(), nil)

	paddedHeader := make([]byte, exportHeaderAlign)
	copy(paddedHeader, headerJSON)

	headerLenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(headerLenBytes, uint32(len(headerJSON)))

	result := make([]byte, 0, 4+exportHeaderAlign+len(ciphertext))
	result = append(result, headerLenBytes...)
	result = append(result, paddedHeader...)
	result = append(result, ciphertext...)

	return result, nil
}

// ExportDecryptHeader 从加密文件中读取明文 Header（不解密 Body）
func ExportDecryptHeader(data []byte) (*ExportHeader, error) {
	if len(data) < 4 {
		return nil, ErrExportInvalidFormat
	}

	headerLen := binary.BigEndian.Uint32(data[:4])
	if headerLen > exportHeaderAlign || int(headerLen)+4 > len(data) {
		return nil, ErrExportInvalidFormat
	}

	headerJSON := bytes.TrimRight(data[4:4+headerLen], "\x00")

	var header ExportHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("%w: invalid header JSON: %v", ErrExportInvalidFormat, err)
	}

	if header.Version != 1 {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrExportInvalidFormat, header.Version)
	}

	return &header, nil
}

// ExportDecrypt 完整解密导出文件
func ExportDecrypt(salt1 []byte, data []byte) (*ExportPayload, error) {
	if len(data) < 4+exportHeaderAlign {
		return nil, ErrExportInvalidFormat
	}

	header, err := ExportDecryptHeader(data)
	if err != nil {
		return nil, err
	}

	salt2, err := base64.StdEncoding.DecodeString(header.Salt2)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid salt2: %v", ErrExportInvalidSalt2, err)
	}

	key, err := exportDeriveKey(salt1, salt2)
	if err != nil {
		return nil, err
	}

	ciphertext := data[4+exportHeaderAlign:]

	if len(ciphertext) < gcmNonceSize {
		return nil, ErrExportDecryptionFailed
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher creation failed: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes-gcm creation failed: %w", err)
	}

	nonce := ciphertext[:gcmNonceSize]
	encryptedData := ciphertext[gcmNonceSize:]

	plaintext, err := aesgcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, ErrExportDecryptionFailed
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(plaintext))
	if err != nil {
		return nil, fmt.Errorf("gzip decompression failed: %w", err)
	}
	defer gzipReader.Close()

	decompressed, err := io.ReadAll(gzipReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read decompressed data: %w", err)
	}

	var payload ExportPayload
	if err := json.Unmarshal(decompressed, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	return &payload, nil
}
