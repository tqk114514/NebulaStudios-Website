// Package paths 定义页面路由路径常量和旧版路由重定向映射，可被任意层引用
package paths

const (
	PathHome = "/"

	PathAdmin = "/admin"

	PathPolicy = "/policy"

	PathPolicyPrivacy     = "/policy/privacy"
	PathPolicyPrivacyHash = "/policy#privacy"
	PathPolicyTerms       = "/policy/terms"
	PathPolicyTermsHash   = "/policy#terms"
	PathPolicyCookies     = "/policy/cookies"
	PathPolicyCookiesHash = "/policy#cookies"

	PathAccount = "/account"

	PathAccountLogin     = "/account/login"
	PathAccountRegister  = "/account/register"
	PathAccountVerify    = "/account/verify"
	PathAccountForgot    = "/account/forgot"
	PathAccountDashboard = "/account/dashboard"
	PathAccountLink      = "/account/link"
	PathAccountOAuth     = "/account/oauth"

	AliasPathLogin     = "/login"
	AliasPathRegister  = "/register"
	AliasPathForgot    = "/forgot"
	AliasPathDashboard = "/dashboard"
	AliasPathVerify    = "/verify"
	AliasPathLink      = "/link"
)

// LegacyRedirects 路由别名 → 实际路由的 301 重定向映射
var LegacyRedirects = map[string]string{
	AliasPathLogin:     PathAccountLogin,
	AliasPathRegister:  PathAccountRegister,
	AliasPathForgot:    PathAccountForgot,
	AliasPathDashboard: PathAccountDashboard,

	PathPolicyPrivacy: PathPolicyPrivacyHash,
	PathPolicyTerms:   PathPolicyTermsHash,
	PathPolicyCookies: PathPolicyCookiesHash,
}
