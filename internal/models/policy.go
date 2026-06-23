package models

import "strings"

// PolicyVersionMeta 对应 manifest.json 中每个文件条目的元数据
type PolicyVersionMeta struct {
	UpdateDate    string `json:"update_date"`
	EffectiveDate string `json:"effective_date"`
}

// PolicyManifest 对应 manifest.json 的嵌套结构
// { policyType: { lang: { filename: { update_date, effective_date } } } }
type PolicyManifest map[string]map[string]map[string]PolicyVersionMeta

// GetLatestEffectiveVersion 获取指定政策类型的最新生效版本（跨所有语言）
// 返回版本号（如 "2026-03-24"），如果找不到返回空字符串
func (m PolicyManifest) GetLatestEffectiveVersion(policyType string) string {
	langs, ok := m[policyType]
	if !ok {
		return ""
	}
	latestVersion := ""
	latestDate := ""
	for _, files := range langs {
		for filename, meta := range files {
			if meta.EffectiveDate > latestDate {
				latestDate = meta.EffectiveDate
				latestVersion = strings.TrimSuffix(filename, ".md")
			}
		}
	}
	return latestVersion
}
