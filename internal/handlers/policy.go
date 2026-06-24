// Package handlers 提供政策版本查询与用户同意记录 API。
package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

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

// PolicyVersionResponse /api/policy/versions 响应中的单个版本条目
// 在 manifest 原始字段基础上附加 status，由服务器端基于当前时间计算
type PolicyVersionResponse struct {
	UpdateDate    string   `json:"update_date"`
	EffectiveDate string   `json:"effective_date"`
	Languages     []string `json:"languages"`
	// status 取值：effective（已生效）/ public_notice（公示期）/ scheduled（未进入公示期）
	Status string `json:"status"`
}

// GetPolicyVersions 获取政策版本清单
// 读取 dist/shared/i18n/policy/manifest.json，基于服务器时间给每个版本标记 status，
// 返回扁平结构：{ policyType: { filename: { update_date, effective_date, languages, status } } }
// 前端直接用 status 字段判断生效/公示状态，无需自己计算时间
// GET /api/policy/versions
func (h *PolicyHandler) GetPolicyVersions(c *gin.Context) {
	manifestPath := filepath.Join("dist", "shared", "i18n", "policy", "manifest.json")

	manifest, err := services.LoadPolicyManifest(manifestPath)
	if err != nil {
		utils.LogError("POLICY", "GetPolicyVersions", err, fmt.Sprintf("Failed to read manifest: %s", manifestPath))
		utils.HTTPErrorResponse(c, "POLICY", http.StatusInternalServerError, "MANIFEST_NOT_FOUND", "Policy manifest not found")
		return
	}

	// 基于服务器时间给每个版本标记状态
	now := time.Now().Format("2006-01-02")
	result := make(map[string]map[string]PolicyVersionResponse)
	for policyType, versions := range manifest {
		result[policyType] = make(map[string]PolicyVersionResponse)
		for filename, meta := range versions {
			status := "effective"
			if meta.EffectiveDate > now {
				if meta.UpdateDate <= now {
					status = "public_notice"
				} else {
					status = "scheduled"
				}
			}
			result[policyType][filename] = PolicyVersionResponse{
				UpdateDate:    meta.UpdateDate,
				EffectiveDate: meta.EffectiveDate,
				Languages:     meta.Languages,
				Status:        status,
			}
		}
	}

	utils.RespondSuccessWithData(c, result)
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
	now := time.Now().Format("2006-01-02")
	for _, policyType := range []string{models.PolicyTypePrivacy, models.PolicyTypeTerms} {
		latestEffective := manifest.GetLatestEffectiveVersion(policyType, now)
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
	now := time.Now().Format("2006-01-02")
	validTypes := map[string]bool{models.PolicyTypePrivacy: true, models.PolicyTypeTerms: true}
	for _, p := range req.Policies {
		if !validTypes[p.PolicyType] {
			utils.HTTPErrorResponse(c, "POLICY", http.StatusBadRequest, "INVALID_POLICY_TYPE", fmt.Sprintf("Invalid policy type: %s", p.PolicyType))
			return
		}
		latestEffective := manifest.GetLatestEffectiveVersion(p.PolicyType, now)
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
	versions, ok := manifest[policyType]
	if !ok {
		return ""
	}
	filename := version + ".md"
	if meta, exists := versions[filename]; exists {
		return meta.EffectiveDate
	}
	return ""
}
