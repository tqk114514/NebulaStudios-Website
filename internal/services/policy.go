package services

import (
	"encoding/json"
	"os"

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
