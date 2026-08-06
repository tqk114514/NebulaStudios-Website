package services

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestPolicyNowFormat(t *testing.T) {
	got := PolicyNow()
	// 断言格式 YYYY-MM-DD
	if len(got) != 10 || got[4] != '-' || got[7] != '-' {
		t.Errorf("PolicyNow() = %q, want YYYY-MM-DD format", got)
	}
}

func TestLoadPolicyManifest(t *testing.T) {
	dir := t.TempDir()

	valid := filepath.Join(dir, "valid.json")
	os.WriteFile(valid, []byte(`{"privacy":{"v1.md":{"update_date":"2026-01-01","effective_date":"2026-02-01"}}}`), 0o644)
	if _, err := LoadPolicyManifest(valid); err != nil {
		t.Fatalf("LoadPolicyManifest(valid) error = %v", err)
	}

	invalid := filepath.Join(dir, "invalid.json")
	os.WriteFile(invalid, []byte(`{not json`), 0o644)
	if _, err := LoadPolicyManifest(invalid); err == nil {
		t.Error("LoadPolicyManifest(invalid JSON) should error")
	}

	if _, err := LoadPolicyManifest(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("LoadPolicyManifest(missing) should error")
	}
}

func TestGetPublicNoticePolicies(t *testing.T) {
	dir := t.TempDir()

	// 构造一个当前在公示期的版本：update <= 今天 < effective
	now := PolicyNow()
	updateDate := now[:8] + "01" // 本月 1 号
	effectiveDate := "2099-01-01"

	manifest := `{"privacy":{"v2.md":{"update_date":"` + updateDate + `","effective_date":"` + effectiveDate + `"}}}`
	path := filepath.Join(dir, "manifest.json")
	os.WriteFile(path, []byte(manifest), 0o644)

	notices, err := GetPublicNoticePolicies(path)
	if err != nil {
		t.Fatalf("GetPublicNoticePolicies() error = %v", err)
	}
	if len(notices) != 1 || notices[0].Version != "v2" {
		t.Errorf("GetPublicNoticePolicies() = %+v, want privacy/v2 in notice", notices)
	}
}

func TestGetPublicNoticePoliciesNone(t *testing.T) {
	dir := t.TempDir()
	// 无公示期版本（effective 在过去）
	manifest := `{"privacy":{"v1.md":{"update_date":"2020-01-01","effective_date":"2020-02-01"}}}`
	path := filepath.Join(dir, "manifest.json")
	os.WriteFile(path, []byte(manifest), 0o644)

	notices, err := GetPublicNoticePolicies(path)
	if err != nil {
		t.Fatalf("GetPublicNoticePolicies() error = %v", err)
	}
	if notices == nil || len(notices) != 0 {
		t.Errorf("GetPublicNoticePolicies() = %v, want empty non-nil list", notices)
	}
}

func TestCompressBrotliRoundTrip(t *testing.T) {
	// 压缩后可被 brotli 解码回原内容（与静态资源预压缩链路一致）
	original := []byte("hello brotli compress me please")
	compressed, err := CompressBrotli(original)
	if err != nil {
		t.Fatalf("CompressBrotli() error = %v", err)
	}
	if len(compressed) == 0 {
		t.Fatal("CompressBrotli() returned empty output")
	}

	decoded, err := brotliDecompress(compressed)
	if err != nil {
		t.Fatalf("brotli decode error = %v", err)
	}
	if string(decoded) != string(original) {
		t.Errorf("round-trip mismatch: got %q, want %q", decoded, original)
	}
}

func TestCompressBrotliEmpty(t *testing.T) {
	compressed, err := CompressBrotli(nil)
	if err != nil {
		t.Fatalf("CompressBrotli(nil) error = %v", err)
	}
	if len(compressed) == 0 {
		t.Error("CompressBrotli(nil) = empty, want non-empty (brotli header)")
	}
}

// brotliDecompress 用 brotli 解码验证压缩链路
func brotliDecompress(data []byte) ([]byte, error) {
	return io.ReadAll(brotli.NewReader(bytes.NewReader(data)))
}
