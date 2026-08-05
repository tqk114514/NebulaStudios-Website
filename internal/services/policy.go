package services

import (
	"encoding/json"
	"os"
	"time"

	"auth-system/internal/models"
)


// PolicyNow 返回政策计算使用的当前日期（北京时间，YYYY-MM-DD）。
// 政策日期均以北京时间为准，而服务器容器时区可能为 UTC，故固定使用东八区计算。
func PolicyNow() string {
	return time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
}

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
	now := PolicyNow()
	result := manifest.GetPublicNoticeVersions(now)
	if result == nil {
		result = []models.PublicNoticePolicy{}
	}
	return result, nil
}
