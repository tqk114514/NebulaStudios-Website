package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"auth-system/internal/config"
	"auth-system/internal/utils"

	"github.com/andybalholm/brotli"
)

// ErrStorageNotInitialized 存储服务未初始化
var ErrStorageNotInitialized = errors.New("storage service not initialized")

// LocalStorageService 本地文件存储服务（替代 R2）
type LocalStorageService struct {
	dir          string
	baseURL      string
	imgProcessor *ImgProcessor
}

// NewLocalStorageService 创建本地存储服务
func NewLocalStorageService(cfg *config.Config) (*LocalStorageService, error) {
	dir := cfg.AvatarDir
	if dir == "" {
		dir = "./data/avatars"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create avatar dir: %w", err)
	}

	imgProcessor := NewImgProcessor()

	utils.LogInfo("STORAGE", fmt.Sprintf("Local storage initialized: dir=%s", dir))
	return &LocalStorageService{
		dir:          dir,
		baseURL:      cfg.BaseURL,
		imgProcessor: imgProcessor,
	}, nil
}

// UploadAvatar 处理图片并保存到本地，返回完整 URL
func (s *LocalStorageService) UploadAvatar(_ context.Context, userUID string, imageData []byte) (string, error) {
	if s == nil {
		return "", ErrStorageNotInitialized
	}
	if s.imgProcessor == nil || !s.imgProcessor.IsAvailable() {
		return "", fmt.Errorf("image processor not available")
	}

	webpData, err := s.imgProcessor.ToWebP(imageData)
	if err != nil {
		return "", fmt.Errorf("failed to process image: %w", err)
	}
	utils.LogInfo("STORAGE", "Image processed by Zig")

	filename := fmt.Sprintf("%s.webp", userUID)
	path := filepath.Join(s.dir, filename)
	if err := os.WriteFile(path, webpData, 0o644); err != nil {
		return "", fmt.Errorf("failed to write avatar file: %w", err)
	}

	avatarURL := fmt.Sprintf("%s/avatars/%s", s.baseURL, filename)
	utils.LogInfo("STORAGE", fmt.Sprintf("Avatar saved: userUID=%s, url=%s, size=%d bytes", userUID, avatarURL, len(webpData)))

	// 生成 Brotli 压缩副本（失败不影响主文件，与 dist 静态资源一致）
	if brData, err := CompressBrotli(webpData); err == nil {
		if werr := os.WriteFile(path+".br", brData, 0o644); werr != nil {
			utils.LogWarn("STORAGE", "Failed to write brotli avatar", fmt.Sprintf("userUID=%s", userUID))
		}
	} else {
		utils.LogWarn("STORAGE", "Failed to compress avatar", fmt.Sprintf("userUID=%s: %v", userUID, err))
	}

	return avatarURL, nil
}

// CompressBrotli 使用 Brotli 压缩数据
func CompressBrotli(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := brotli.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DeleteAvatar 删除用户头像
func (s *LocalStorageService) DeleteAvatar(_ context.Context, userUID string) error {
	if s == nil {
		return ErrStorageNotInitialized
	}

	filename := fmt.Sprintf("%s.webp", userUID)
	path := filepath.Join(s.dir, filename)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete avatar file: %w", err)
	}
	// 同步删除 Brotli 压缩副本（不存在则忽略）
	if err := os.Remove(path + ".br"); err != nil && !os.IsNotExist(err) {
		utils.LogWarn("STORAGE", "Failed to delete brotli avatar", fmt.Sprintf("userUID=%s", userUID))
	}
	utils.LogInfo("STORAGE", fmt.Sprintf("Avatar deleted: userUID=%s", userUID))
	return nil
}

// IsConfigured 本地存储始终可用
func (s *LocalStorageService) IsConfigured() bool {
	return s != nil
}

// GetImgProcessor 获取图片处理器实例（用于优雅关闭）
func (s *LocalStorageService) GetImgProcessor() ImageProcessor {
	if s == nil {
		return nil
	}
	return s.imgProcessor
}
