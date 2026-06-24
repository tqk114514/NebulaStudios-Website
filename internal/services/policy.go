package services

import (
	"encoding/json"
	"os"
	"time"

	"auth-system/internal/models"
)

// LoadPolicyManifest 从文件加载政策清单
func LoadPolicyManifest(path string) (models.PolicyManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest models.PolicyManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

// GetPublicNoticePolicies 获取当前在公示期的政策版本列表
func GetPublicNoticePolicies(manifestPath string) ([]models.PublicNoticePolicy, error) {
	manifest, err := LoadPolicyManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	now := time.Now().Format("2006-01-02")
	result := manifest.GetPublicNoticeVersions(now)
	if result == nil {
		result = []models.PublicNoticePolicy{}
	}
	return result, nil
}
