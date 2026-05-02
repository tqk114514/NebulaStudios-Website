/**
 * internal/version/version.go
 * 版本信息包
 *
 * 功能：
 * - ServerCommit：编译时通过 ldflags 注入的 Git commit hash
 * - 版本信息 API 数据结构
 *
 * 编译时注入方式：
 *   go build -ldflags "-X auth-system/internal/version.ServerCommit=$(git rev-parse --short HEAD)"
 */

package version

var ServerCommit = "unknown"

const (
	RepoOwner = "tqk114514"
	RepoName  = "NebulaStudios-Website"
)
