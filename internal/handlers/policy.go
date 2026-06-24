// Package handlers 提供政策版本查询与用户同意记录 API。
package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PolicyHandler 政策版本查询与用户同意记录 Handler
type PolicyHandler struct {
	pool *pgxpool.Pool
}

// NewPolicyHandler 创建政策 Handler，验证所有必需依赖后初始化
func NewPolicyHandler(pool *pgxpool.Pool) (*PolicyHandler, error) {
	if pool == nil {
		return nil, errors.New("pool is required")
	}

	utils.LogInfo("POLICY", "PolicyHandler initialized")

	return &PolicyHandler{pool: pool}, nil
}

// GetPolicyVersions 获取政策版本清单
// 读取 dist/shared/i18n/policy/manifest.json 并原样返回其嵌套结构：
// { policyType: { lang: { filename: { update_date, effective_date } } } }
// 后端仅做读取与格式校验，不对结构做扁平化或排序
// GET /api/policy/versions
func (h *PolicyHandler) GetPolicyVersions(c *gin.Context) {
	manifestPath := filepath.Join("dist", "shared", "i18n", "policy", "manifest.json")

	manifest, err := services.LoadPolicyManifest(manifestPath)
	if err != nil {
		utils.LogError("POLICY", "GetPolicyVersions", err, fmt.Sprintf("Failed to read manifest: %s", manifestPath))
		utils.HTTPErrorResponse(c, "POLICY", http.StatusInternalServerError, "MANIFEST_NOT_FOUND", "Policy manifest not found")
		return
	}

	utils.RespondSuccessWithData(c, manifest)
}

// GetPublicNoticePolicies 返回当前在公示期的政策版本
// GET /api/policy/public-notice
func (h *PolicyHandler) GetPublicNoticePolicies(c *gin.Context) {
	manifestPath := filepath.Join("dist", "shared", "i18n", "policy", "manifest.json")

	policies, err := services.GetPublicNoticePolicies(manifestPath)
	if err != nil {
		utils.LogError("POLICY", "GetPublicNoticePolicies", err, fmt.Sprintf("Failed to read manifest: %s", manifestPath))
		utils.HTTPErrorResponse(c, "POLICY", http.StatusInternalServerError, "MANIFEST_NOT_FOUND", "Policy manifest not found")
		return
	}

	utils.RespondSuccessWithData(c, policies)
}

// PendingConsentPolicy 表示一个需要用户同意的已生效政策版本
type PendingConsentPolicy struct {
	PolicyType    string `json:"policy_type"`
	Version       string `json:"version"`
	EffectiveDate string `json:"effective_date"`
}

// consentRequestPolicy 需要同意的政策条目（请求体）
type consentRequestPolicy struct {
	PolicyType    string `json:"policy_type"`
	PolicyVersion string `json:"policy_version"`
}

// GetPendingConsent 返回当前用户尚未同意的已生效政策版本（隐私政策 + 服务条款）
// 比对 user_consents 表中用户最新同意版本与 manifest 中最新生效版本，不一致则需重新同意
// GET /api/policy/pending-consent
func (h *PolicyHandler) GetPendingConsent(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok || userUID == "" {
		utils.HTTPErrorResponse(c, "POLICY", http.StatusUnauthorized, "UNAUTHORIZED", "GetPendingConsent called without valid userUID")
		return
	}

	manifestPath := filepath.Join("dist", "shared", "i18n", "policy", "manifest.json")
	manifest, err := services.LoadPolicyManifest(manifestPath)
	if err != nil {
		utils.LogError("POLICY", "GetPendingConsent", err, fmt.Sprintf("Failed to read manifest: %s", manifestPath))
		utils.HTTPErrorResponse(c, "POLICY", http.StatusInternalServerError, "MANIFEST_NOT_FOUND", "Policy manifest not found")
		return
	}

	ctx := c.Request.Context()
	consentRepo := models.NewUserConsentRepository(h.pool)
	consents, err := consentRepo.FindByUserUID(ctx, userUID)
	if err != nil {
		utils.LogError("POLICY", "GetPendingConsent", err, fmt.Sprintf("Failed to query consents: userUID=%s", userUID))
		utils.HTTPErrorResponse(c, "POLICY", http.StatusInternalServerError, "DATABASE_ERROR", "Failed to query user consents")
		return
	}

	// 用户每个政策类型最新同意的版本（FindByUserUID 已按 created_at DESC 排序）
	latestConsented := make(map[string]string)
	for _, c := range consents {
		if _, exists := latestConsented[c.PolicyType]; !exists {
			latestConsented[c.PolicyType] = c.PolicyVersion
		}
	}

	var pending []PendingConsentPolicy
	for _, policyType := range []string{models.PolicyTypePrivacy, models.PolicyTypeTerms} {
		latestEffective := manifest.GetLatestEffectiveVersion(policyType)
		if latestEffective == "" {
			continue
		}
		if latestConsented[policyType] != latestEffective {
			// 获取生效日期用于前端展示
			effectiveDate := h.getPolicyEffectiveDate(manifest, policyType, latestEffective)
			pending = append(pending, PendingConsentPolicy{
				PolicyType:    policyType,
				Version:       latestEffective,
				EffectiveDate: effectiveDate,
			})
		}
	}

	if pending == nil {
		pending = []PendingConsentPolicy{}
	}

	utils.RespondSuccessWithData(c, gin.H{"policies": pending})
}

// RecordConsent 记录用户对当前生效政策版本的同意
// 仅接受当前最新生效版本，防止写入过期版本
// POST /api/policy/consent
func (h *PolicyHandler) RecordConsent(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok || userUID == "" {
		utils.HTTPErrorResponse(c, "POLICY", http.StatusUnauthorized, "UNAUTHORIZED", "RecordConsent called without valid userUID")
		return
	}

	var req struct {
		Policies []consentRequestPolicy `json:"policies"`
	}
	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "POLICY", http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body for RecordConsent")
		return
	}

	if len(req.Policies) == 0 {
		utils.HTTPErrorResponse(c, "POLICY", http.StatusBadRequest, "MISSING_PARAMETERS", "Empty policies in RecordConsent")
		return
	}

	manifestPath := filepath.Join("dist", "shared", "i18n", "policy", "manifest.json")
	manifest, err := services.LoadPolicyManifest(manifestPath)
	if err != nil {
		utils.LogError("POLICY", "RecordConsent", err, fmt.Sprintf("Failed to read manifest: %s", manifestPath))
		utils.HTTPErrorResponse(c, "POLICY", http.StatusInternalServerError, "MANIFEST_NOT_FOUND", "Policy manifest not found")
		return
	}

	// 验证每个条目：policy_type 必须是 privacy/terms，policy_version 必须是当前最新生效版本
	validTypes := map[string]bool{models.PolicyTypePrivacy: true, models.PolicyTypeTerms: true}
	for _, p := range req.Policies {
		if !validTypes[p.PolicyType] {
			utils.HTTPErrorResponse(c, "POLICY", http.StatusBadRequest, "INVALID_POLICY_TYPE", fmt.Sprintf("Invalid policy type: %s", p.PolicyType))
			return
		}
		latestEffective := manifest.GetLatestEffectiveVersion(p.PolicyType)
		if latestEffective == "" || p.PolicyVersion != latestEffective {
			utils.HTTPErrorResponse(c, "POLICY", http.StatusBadRequest, "INVALID_POLICY_VERSION", fmt.Sprintf("Policy version mismatch: type=%s, requested=%s, latest=%s", p.PolicyType, p.PolicyVersion, latestEffective))
			return
		}
	}

	ctx := c.Request.Context()
	consentRepo := models.NewUserConsentRepository(h.pool)
	for _, p := range req.Policies {
		if err := consentRepo.LogConsent(ctx, userUID, p.PolicyType, p.PolicyVersion); err != nil {
			utils.LogError("POLICY", "RecordConsent", err, fmt.Sprintf("Failed to log consent: userUID=%s, type=%s, version=%s", userUID, p.PolicyType, p.PolicyVersion))
			utils.HTTPErrorResponse(c, "POLICY", http.StatusInternalServerError, "CONSENT_LOG_FAILED", "Failed to record consent")
			return
		}
	}

	utils.LogInfo("POLICY", fmt.Sprintf("Policy consent recorded: userUID=%s, count=%d", userUID, len(req.Policies)))
	utils.RespondSuccess(c, gin.H{"message": "Consent recorded"})
}

// getPolicyEffectiveDate 从 manifest 获取指定政策类型的版本生效日期
func (h *PolicyHandler) getPolicyEffectiveDate(manifest models.PolicyManifest, policyType, version string) string {
	langs, ok := manifest[policyType]
	if !ok {
		return ""
	}
	filename := version + ".md"
	for _, files := range langs {
		if meta, exists := files[filename]; exists {
			return meta.EffectiveDate
		}
	}
	return ""
}
