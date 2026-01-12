/**
 * internal/services/r2.go
 * Cloudflare R2 存储服务
 *
 * 功能：
 * - 上传文件到 R2
 * - 删除 R2 文件
 * - 头像上传（WebP 转换）
 */

package services

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"auth-system/internal/config"
	"auth-system/internal/utils"

	"github.com/HugoSmits86/nativewebp"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// R2Service R2 存储服务
type R2Service struct {
	client       *s3.Client
	bucket       string
	url          string
	imgProcessor *ImgProcessor
}

// NewR2Service 创建 R2 服务实例
func NewR2Service() (*R2Service, error) {
	cfg := config.Get()

	if cfg.R2Endpoint == "" || cfg.R2AccessKey == "" || cfg.R2SecretKey == "" || cfg.R2Bucket == "" {
		utils.LogPrintf("[R2] WARN: R2 not configured, avatar upload will be disabled")
		return nil, nil
	}

	r2Resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL: cfg.R2Endpoint,
		}, nil
	})

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithEndpointResolverWithOptions(r2Resolver),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.R2AccessKey,
			cfg.R2SecretKey,
			"",
		)),
		awsconfig.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load R2 config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg)

	// 初始化图片处理器
	imgProcessor := NewImgProcessor()

	utils.LogPrintf("[R2] R2 service initialized: bucket=%s", cfg.R2Bucket)

	return &R2Service{
		client:       client,
		bucket:       cfg.R2Bucket,
		url:          cfg.R2URL,
		imgProcessor: imgProcessor,
	}, nil
}


// UploadAvatar 上传头像到 R2
// 将图片转换为 WebP 格式后上传
//
// 参数：
//   - ctx: 上下文
//   - userID: 用户 ID
//   - imageData: 图片二进制数据
//
// 返回：
//   - string: 头像 URL
//   - error: 错误信息
func (s *R2Service) UploadAvatar(ctx context.Context, userID int64, imageData []byte) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("R2 service not initialized")
	}

	var webpData []byte

	// 优先使用 Rust 处理器
	if s.imgProcessor != nil && s.imgProcessor.IsAvailable() {
		data, err := s.imgProcessor.ToWebP(imageData)
		if err != nil {
			utils.LogPrintf("[R2] WARN: Rust processor failed, falling back to Go: %v", err)
		} else {
			webpData = data
			utils.LogPrintf("[R2] Image processed by Rust")
		}
	}

	// 降级到 Go 处理
	if webpData == nil {
		img, _, err := image.Decode(bytes.NewReader(imageData))
		if err != nil {
			return "", fmt.Errorf("failed to decode image: %w", err)
		}

		var webpBuf bytes.Buffer
		if err := nativewebp.Encode(&webpBuf, img, nil); err != nil {
			return "", fmt.Errorf("failed to encode webp: %w", err)
		}
		webpData = webpBuf.Bytes()
		utils.LogPrintf("[R2] Image processed by Go (fallback)")
	}

	// 上传到 R2
	key := fmt.Sprintf("avatar/%d.webp", userID)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(webpData),
		ContentType: aws.String("image/webp"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload to R2: %w", err)
	}

	avatarURL := fmt.Sprintf("%s/%s", s.url, key)
	utils.LogPrintf("[R2] Avatar uploaded: userID=%d, url=%s, size=%d bytes", userID, avatarURL, len(webpData))

	return avatarURL, nil
}

// DeleteAvatar 删除用户头像
//
// 参数：
//   - ctx: 上下文
//   - userID: 用户 ID
//
// 返回：
//   - error: 错误信息
func (s *R2Service) DeleteAvatar(ctx context.Context, userID int64) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("R2 service not initialized")
	}

	key := fmt.Sprintf("avatar/%d.webp", userID)
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete from R2: %w", err)
	}

	utils.LogPrintf("[R2] Avatar deleted: userID=%d", userID)
	return nil
}

// IsConfigured 检查 R2 是否已配置
func (s *R2Service) IsConfigured() bool {
	return s != nil && s.client != nil
}

// GetImgProcessor 获取图片处理器实例（用于优雅关闭）
func (s *R2Service) GetImgProcessor() *ImgProcessor {
	if s == nil {
		return nil
	}
	return s.imgProcessor
}
