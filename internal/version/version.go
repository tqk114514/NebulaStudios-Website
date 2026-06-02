// Package version 提供编译时注入的版本信息和 GitHub 最新 commit 缓存
package version

// ServerCommit 编译时通过 ldflags 注入的 Git commit hash，未注入时默认 "unknown"
var ServerCommit = "unknown"

const (
	RepoOwner = "tqk114514"
	RepoName  = "NebulaStudios-Website"
)
