package models

import (
	"sort"
	"strings"
)

// PolicyVersionMeta 对应 manifest.json 中每个文件条目的元数据
type PolicyVersionMeta struct {
	UpdateDate    string `json:"update_date"`
	EffectiveDate string `json:"effective_date"`
}

// PolicyManifest 对应 manifest.json 的嵌套结构
// { policyType: { lang: { filename: { update_date, effective_date } } } }
type PolicyManifest map[string]map[string]map[string]PolicyVersionMeta

// PublicNoticePolicy 表示一个正在公示期的政策版本
type PublicNoticePolicy struct {
	PolicyType    string `json:"policy_type"`
	Version       string `json:"version"`
	UpdateDate    string `json:"update_date"`
	EffectiveDate string `json:"effective_date"`
}

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

// GetPublicNoticeVersions 返回当前在公示期的政策版本（update_date <= now < effective_date）
// 对每个政策类型，只返回最新公示期版本（update_date 最大的）
// now 格式为 YYYY-MM-DD
func (m PolicyManifest) GetPublicNoticeVersions(now string) []PublicNoticePolicy {
	var result []PublicNoticePolicy
	for policyType, langs := range m {
		// 收集该类型所有在公示期的版本（去重，同一版本号跨语言的 meta 应一致）
		versionMap := make(map[string]PolicyVersionMeta)
		for _, files := range langs {
			for filename, meta := range files {
				if meta.UpdateDate <= now && now < meta.EffectiveDate {
					version := strings.TrimSuffix(filename, ".md")
					versionMap[version] = meta
				}
			}
		}
		// 找最新的公示期版本（update_date 最大的）
		var latestVersion string
		var latestMeta PolicyVersionMeta
		for version, meta := range versionMap {
			if latestVersion == "" || meta.UpdateDate > latestMeta.UpdateDate {
				latestVersion = version
				latestMeta = meta
			}
		}
		if latestVersion != "" {
			result = append(result, PublicNoticePolicy{
				PolicyType:    policyType,
				Version:       latestVersion,
				UpdateDate:    latestMeta.UpdateDate,
				EffectiveDate: latestMeta.EffectiveDate,
			})
		}
	}
	// 排序确保结果顺序一致
	sort.Slice(result, func(i, j int) bool {
		return result[i].PolicyType < result[j].PolicyType
	})
	return result
}
