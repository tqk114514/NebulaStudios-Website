/**
 * internal/paths/paths.go
 * 页面路由路径常量与重定向映射
 *
 * 功能：
 * - 集中管理所有页面路由路径常量
 * - 定义旧版路由 → 实际路由的 301 重定向映射
 * - 路由变更只需修改此文件，全局生效
 *
 * 依赖：
 * - 无（零依赖包，可被任意层引用）
 */

package paths

const (
	// PathHome 首页
	PathHome = "/"

	// PathAdmin 管理后台
	PathAdmin = "/admin"

	// PathPolicy 隐私政策页
	PathPolicy = "/policy"

	// PathPolicyPrivacy 隐私条款页（→ /policy#privacy）
	PathPolicyPrivacy = "/policy/privacy"
	// PathPolicyPrivacyHash 隐私条款锚点
	PathPolicyPrivacyHash = "/policy#privacy"
	// PathPolicyTerms 服务条款页（→ /policy#terms）
	PathPolicyTerms = "/policy/terms"
	// PathPolicyTermsHash 服务条款锚点
	PathPolicyTermsHash = "/policy#terms"
	// PathPolicyCookies Cookie 政策页（→ /policy#cookies）
	PathPolicyCookies = "/policy/cookies"
	// PathPolicyCookiesHash Cookie 政策锚点
	PathPolicyCookiesHash = "/policy#cookies"

	// PathAccount 账户模块根路径（/account → 重定向到登录页）
	PathAccount = "/account"

	// PathAccountLogin 登录页
	PathAccountLogin = "/account/login"
	// PathAccountRegister 注册页
	PathAccountRegister = "/account/register"
	// PathAccountVerify 邮箱验证页
	PathAccountVerify = "/account/verify"
	// PathAccountForgot 忘记密码页
	PathAccountForgot = "/account/forgot"
	// PathAccountDashboard 用户仪表盘
	PathAccountDashboard = "/account/dashboard"
	// PathAccountLink 账户绑定确认页
	PathAccountLink = "/account/link"
	// PathAccountOAuth OAuth 回调页
	PathAccountOAuth = "/account/oauth"

	// ---- 以下为旧版别名（301 重定向到上面的实际路径）----

	// AliasPathLogin /login → /account/login
	AliasPathLogin = "/login"
	// AliasPathRegister /register → /account/register
	AliasPathRegister = "/register"
	// AliasPathForgot /forgot → /account/forgot
	AliasPathForgot = "/forgot"
	// AliasPathDashboard /dashboard → /account/dashboard
	AliasPathDashboard = "/dashboard"
	// AliasPathVerify /verify → /account/verify
	AliasPathVerify = "/verify"
	// AliasPathLink /link → /account/link
	AliasPathLink = "/link"
)

// LegacyRedirects 旧版路由 → 实际路由的 301 重定向映射
// 新增旧路由只需在此添加一行，routes.go 自动注册
var LegacyRedirects = map[string]string{
	// 账户模块旧版短路径
	AliasPathLogin:     PathAccountLogin,
	AliasPathRegister:  PathAccountRegister,
	AliasPathForgot:    PathAccountForgot,
	AliasPathDashboard: PathAccountDashboard,

	// Policy 子路径 → 锚点跳转
	PathPolicyPrivacy: PathPolicyPrivacyHash,
	PathPolicyTerms:   PathPolicyTermsHash,
	PathPolicyCookies: PathPolicyCookiesHash,
}
