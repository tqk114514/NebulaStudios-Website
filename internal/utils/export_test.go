package utils

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
)

func testSalt1(t *testing.T) []byte {
	t.Helper()
	salt1, err := ParseExportSalt1(base64.StdEncoding.EncodeToString([]byte("a-very-secret-salt-value")))
	if err != nil {
		t.Fatalf("ParseExportSalt1: %v", err)
	}
	return salt1
}

// buildExportFile 按导出格式拼装 [4字节头长度][256字节对齐头][密文]
func buildExportFile(headerJSON []byte, ciphertext []byte) []byte {
	padded := make([]byte, exportHeaderAlign)
	copy(padded, headerJSON)

	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, uint32(len(headerJSON)))
	out = append(out, padded...)
	return append(out, ciphertext...)
}

func TestParseExportSalt1(t *testing.T) {
	if _, err := ParseExportSalt1(""); !errors.Is(err, ErrExportInvalidSalt1) {
		t.Errorf("empty salt error = %v, want ErrExportInvalidSalt1", err)
	}
	if _, err := ParseExportSalt1("not-base64!!"); !errors.Is(err, ErrExportInvalidSalt1) {
		t.Errorf("invalid base64 error = %v, want ErrExportInvalidSalt1", err)
	}
	// 合法 base64 但解码后为空
	if _, err := ParseExportSalt1(""); !errors.Is(err, ErrExportInvalidSalt1) {
		t.Errorf("empty decode error = %v, want ErrExportInvalidSalt1", err)
	}

	raw := []byte("salt-material")
	got, err := ParseExportSalt1(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("ParseExportSalt1 error = %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("salt = %x, want %x", got, raw)
	}
}

func TestGenerateExportSalt2(t *testing.T) {
	a := GenerateExportSalt2()
	b := GenerateExportSalt2()

	if len(a) != exportSalt2Size {
		t.Fatalf("len = %d, want %d", len(a), exportSalt2Size)
	}
	if bytes.Equal(a, b) {
		t.Error("two generated salt2 must not be identical")
	}
}

func TestExportDeriveKey(t *testing.T) {
	salt1 := []byte("salt-one")
	salt2 := GenerateExportSalt2()

	k1, err := exportDeriveKey(salt1, salt2)
	if err != nil {
		t.Fatalf("exportDeriveKey: %v", err)
	}
	if len(k1) != aesKeySize {
		t.Fatalf("key len = %d, want %d", len(k1), aesKeySize)
	}

	k2, err := exportDeriveKey(salt1, salt2)
	if err != nil {
		t.Fatalf("exportDeriveKey: %v", err)
	}
	if !bytes.Equal(k1, k2) {
		t.Error("key derivation must be deterministic for identical inputs")
	}

	k3, err := exportDeriveKey(salt1, GenerateExportSalt2())
	if err != nil {
		t.Fatalf("exportDeriveKey: %v", err)
	}
	if bytes.Equal(k1, k3) {
		t.Error("different salt2 must produce different keys")
	}
}

func TestExportEncryptDecryptRoundTrip(t *testing.T) {
	salt1 := testSalt1(t)
	salt2 := GenerateExportSalt2()

	header := &ExportHeader{
		Version:    1,
		ExportedAt: "2026-09-12T00:00:00Z",
		ExportedBy: "admin-uid",
		UsersCount: 1,
		LogsCount:  1,
	}
	payload := &ExportPayload{
		Users:    []map[string]any{{"uid": "u1", "email": "u1@example.com"}},
		UserLogs: []map[string]any{{"uid": "u1", "action": "login"}},
	}

	data, err := ExportEncrypt(salt1, salt2, header, payload)
	if err != nil {
		t.Fatalf("ExportEncrypt: %v", err)
	}
	if len(data) <= 4+exportHeaderAlign {
		t.Fatalf("encrypted size = %d, want header + ciphertext", len(data))
	}

	// 明文头应可直接读取（无需解密 body）
	gotHeader, err := ExportDecryptHeader(data)
	if err != nil {
		t.Fatalf("ExportDecryptHeader: %v", err)
	}
	if gotHeader.ExportedBy != "admin-uid" || gotHeader.UsersCount != 1 || gotHeader.LogsCount != 1 {
		t.Errorf("header = %+v, want metadata preserved", gotHeader)
	}
	if gotHeader.Salt2 == "" {
		t.Error("header.Salt2 should be filled by ExportEncrypt")
	}

	// 完整解密
	gotPayload, err := ExportDecrypt(salt1, data)
	if err != nil {
		t.Fatalf("ExportDecrypt: %v", err)
	}
	if len(gotPayload.Users) != 1 || gotPayload.Users[0]["uid"] != "u1" {
		t.Errorf("users = %+v, want single user u1", gotPayload.Users)
	}
	if len(gotPayload.UserLogs) != 1 || gotPayload.UserLogs[0]["action"] != "login" {
		t.Errorf("user_logs = %+v, want single login entry", gotPayload.UserLogs)
	}

	// 错误的 salt1 应解密失败（GCM 认证失败），而不是返回脏数据
	if _, err := ExportDecrypt([]byte("wrong-salt"), data); !errors.Is(err, ErrExportDecryptionFailed) {
		t.Errorf("decrypt with wrong salt error = %v, want ErrExportDecryptionFailed", err)
	}
}

func TestExportDecryptHeaderErrors(t *testing.T) {
	if _, err := ExportDecryptHeader([]byte{1, 2}); !errors.Is(err, ErrExportInvalidFormat) {
		t.Errorf("short data error = %v, want ErrExportInvalidFormat", err)
	}

	// 头长度字段超出对齐长度
	oversized := make([]byte, 8)
	binary.BigEndian.PutUint32(oversized, uint32(exportHeaderAlign+1))
	if _, err := ExportDecryptHeader(oversized); !errors.Is(err, ErrExportInvalidFormat) {
		t.Errorf("oversized header error = %v, want ErrExportInvalidFormat", err)
	}

	// 头长度合法但 JSON 非法
	badJSON := buildExportFile([]byte("{not json"), nil)
	if _, err := ExportDecryptHeader(badJSON); !errors.Is(err, ErrExportInvalidFormat) {
		t.Errorf("invalid header JSON error = %v, want ErrExportInvalidFormat", err)
	}

	// 版本不受支持（ExportEncrypt 同样要求 Version=1 才能被解出）
	oldHeader, err := json.Marshal(&ExportHeader{Version: 2, Salt2: base64.StdEncoding.EncodeToString(GenerateExportSalt2())})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	if _, err := ExportDecryptHeader(buildExportFile(oldHeader, nil)); !errors.Is(err, ErrExportInvalidFormat) {
		t.Errorf("unsupported version error = %v, want ErrExportInvalidFormat", err)
	}
}

func TestExportDecryptErrors(t *testing.T) {
	salt1 := testSalt1(t)

	if _, err := ExportDecrypt(salt1, make([]byte, 10)); !errors.Is(err, ErrExportInvalidFormat) {
		t.Errorf("short file error = %v, want ErrExportInvalidFormat", err)
	}

	// 头中 salt2 不是合法 base64
	badSalt2, err := json.Marshal(&ExportHeader{Version: 1, Salt2: "!!!not-base64!!!"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	if _, err := ExportDecrypt(salt1, buildExportFile(badSalt2, make([]byte, 64))); !errors.Is(err, ErrExportInvalidSalt2) {
		t.Errorf("invalid salt2 error = %v, want ErrExportInvalidSalt2", err)
	}

	// 密文短于 nonce
	header, err := json.Marshal(&ExportHeader{Version: 1, Salt2: base64.StdEncoding.EncodeToString(GenerateExportSalt2())})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	if _, err := ExportDecrypt(salt1, buildExportFile(header, []byte{1, 2})); !errors.Is(err, ErrExportDecryptionFailed) {
		t.Errorf("truncated ciphertext error = %v, want ErrExportDecryptionFailed", err)
	}

	// 密文被篡改
	tampered := buildExportFile(header, bytes.Repeat([]byte{0xAB}, gcmNonceSize+32))
	if _, err := ExportDecrypt(salt1, tampered); !errors.Is(err, ErrExportDecryptionFailed) {
		t.Errorf("tampered ciphertext error = %v, want ErrExportDecryptionFailed", err)
	}
}
