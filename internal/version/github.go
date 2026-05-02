/**
 * internal/version/github.go
 * GitHub API 版本获取与缓存
 *
 * 功能：
 * - 获取仓库 main 分支最新 commit SHA
 * - 内存缓存（10 分钟 TTL）避免频繁 API 调用
 * - 优雅降级：API 不可用时返回缓存值或 "unknown"
 */

package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type cachedCommit struct {
	sha       string
	fetchedAt time.Time
}

var (
	repoCommitCache   *cachedCommit
	repoCommitCacheMu sync.Mutex
	cacheTTL          = 1 * time.Minute
)

type githubCommit struct {
	SHA string `json:"sha"`
}

func GetRepoCommit() string {
	repoCommitCacheMu.Lock()

	if repoCommitCache != nil && time.Since(repoCommitCache.fetchedAt) < cacheTTL {
		sha := repoCommitCache.sha
		repoCommitCacheMu.Unlock()
		return sha
	}

	repoCommitCacheMu.Unlock()

	sha := fetchLatestCommit()
	if sha == "" {
		repoCommitCacheMu.Lock()
		if repoCommitCache != nil {
			sha = repoCommitCache.sha
		}
		repoCommitCacheMu.Unlock()
		if sha == "" {
			sha = "unknown"
		}
		return sha
	}

	shortSHA := sha
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}

	repoCommitCacheMu.Lock()
	repoCommitCache = &cachedCommit{
		sha:       shortSHA,
		fetchedAt: time.Now(),
	}
	repoCommitCacheMu.Unlock()

	return shortSHA
}

func fetchLatestCommit() string {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/main", RepoOwner, RepoName)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var commit githubCommit
	if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil {
		return ""
	}

	if commit.SHA == "" {
		return ""
	}

	return commit.SHA
}
