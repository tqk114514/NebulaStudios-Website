package services

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"auth-system/internal/config"

	"github.com/andybalholm/brotli"
)

// chdirTemp 切换到临时目录，避免测试在仓库里留下 data/avatars
func chdirTemp(t *testing.T) string {
	t.Helper()

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	return dir
}

func TestNewLocalStorageServiceCreatesDir(t *testing.T) {
	dir := chdirTemp(t)
	target := filepath.Join(dir, "avatars")

	svc, err := NewLocalStorageService(&config.Config{AvatarDir: target})
	if err != nil {
		t.Fatalf("NewLocalStorageService: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("avatar dir not created: %v", err)
	}
	if !svc.IsConfigured() {
		t.Error("service should be configured")
	}
}

// AvatarDir 为空时回落到默认目录
func TestNewLocalStorageServiceDefaultDir(t *testing.T) {
	dir := chdirTemp(t)

	if _, err := NewLocalStorageService(&config.Config{}); err != nil {
		t.Fatalf("NewLocalStorageService with default dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "avatars")); err != nil {
		t.Errorf("default avatar dir not created: %v", err)
	}
}

func TestUploadAvatarGuards(t *testing.T) {
	// nil 接收者
	var nilSvc *LocalStorageService
	if _, err := nilSvc.UploadAvatar(context.Background(), "u1", []byte("x")); err != ErrStorageNotInitialized {
		t.Errorf("nil service error = %v, want ErrStorageNotInitialized", err)
	}

	// 处理器不可用（Windows 上 Unix Socket 不可用时即为此分支）
	svc := &LocalStorageService{dir: t.TempDir(), baseURL: "https://test.local"}
	if _, err := svc.UploadAvatar(context.Background(), "u1", []byte("x")); err == nil {
		t.Error("unavailable image processor should be rejected")
	}
}

func TestCompressBrotli(t *testing.T) {
	compressed, err := CompressBrotli([]byte("hello-brotli"))
	if err != nil {
		t.Fatalf("CompressBrotli: %v", err)
	}
	if len(compressed) == 0 {
		t.Fatal("compressed output should not be empty")
	}

	// 可被标准 Brotli 解码还原，确保与主流程（dist 静态资源）兼容
	reader := brotli.NewReader(bytes.NewReader(compressed))
	var out bytes.Buffer
	if _, err := out.ReadFrom(reader); err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if out.String() != "hello-brotli" {
		t.Errorf("decompressed = %q, want hello-brotli", out.String())
	}

	// 空输入也应安全处理
	if _, err := CompressBrotli(nil); err != nil {
		t.Errorf("CompressBrotli(nil): %v", err)
	}
}

func TestDeleteAvatar(t *testing.T) {
	// nil 接收者
	var nilSvc *LocalStorageService
	if err := nilSvc.DeleteAvatar(context.Background(), "u1"); err != ErrStorageNotInitialized {
		t.Errorf("nil service error = %v, want ErrStorageNotInitialized", err)
	}

	dir := t.TempDir()
	svc := &LocalStorageService{dir: dir, baseURL: "https://test.local"}

	// 文件不存在：幂等成功
	if err := svc.DeleteAvatar(context.Background(), "missing"); err != nil {
		t.Errorf("deleting missing avatar should succeed: %v", err)
	}

	// 存在的文件与其 Brotli 副本都应被删除
	path := filepath.Join(dir, "u1.webp")
	if err := os.WriteFile(path, []byte("webp"), 0o644); err != nil {
		t.Fatalf("write avatar: %v", err)
	}
	if err := os.WriteFile(path+".br", []byte("br"), 0o644); err != nil {
		t.Fatalf("write br: %v", err)
	}

	if err := svc.DeleteAvatar(context.Background(), "u1"); err != nil {
		t.Fatalf("DeleteAvatar: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("avatar file should be removed")
	}
	if _, err := os.Stat(path + ".br"); !os.IsNotExist(err) {
		t.Error("brotli copy should be removed together")
	}
}

func TestIsConfiguredAndGetImgProcessor(t *testing.T) {
	var nilSvc *LocalStorageService
	if nilSvc.IsConfigured() {
		t.Error("nil service should not be configured")
	}
	if nilSvc.GetImgProcessor() != nil {
		t.Error("nil service should return nil image processor")
	}

	svc := &LocalStorageService{}
	if !svc.IsConfigured() {
		t.Error("non-nil service should be configured")
	}
}
