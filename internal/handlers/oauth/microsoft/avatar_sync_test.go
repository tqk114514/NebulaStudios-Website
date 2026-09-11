package microsoft

import (
	"context"
	"database/sql"
	"encoding/base64"
	"testing"
	"time"

	"auth-system/internal/config"
	"auth-system/internal/handlers/oauth"
	"auth-system/internal/models"
	"auth-system/internal/testutil"

	"github.com/gin-gonic/gin"
)

// newHandlerWithStorage 构造带本地存储 fake 的 handler，用于观察头像的异步转存/删除
func newHandlerWithStorage(t *testing.T) (*MicrosoftHandler, *testutil.FakeUserRepo, *testutil.FakeStorageService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	userRepo := testutil.NewFakeUserRepo()
	storage := &testutil.FakeStorageService{Configured: true}

	h, err := NewMicrosoftHandler(
		&config.Config{
			BaseURL:               "https://test.local",
			MicrosoftClientID:     "ms-client-id",
			MicrosoftClientSecret: "ms-client-secret",
			DefaultAvatarURL:      "https://cdn.test/default.png",
		},
		userRepo,
		&testutil.FakeUserLogStore{},
		&testutil.FakeSessionManager{},
		&testutil.FakeUserCache{},
		storage,
	)
	if err != nil {
		t.Fatalf("NewMicrosoftHandler: %v", err)
	}
	return h, userRepo, storage
}

// waitUntil 轮询等待条件成立，避免用固定 sleep 造成偶发失败
func waitUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// 绑定后异步转存头像：原始二进制优先，data URL 作为兜底
func TestAfterLinkStoresAvatar(t *testing.T) {
	h, repo, storage := newHandlerWithStorage(t)
	repo.Seed(&models.User{UID: "u1", MicrosoftAvatarSync: true})
	// data URL 兜底路径
	h2, repo2, storage2 := newHandlerWithStorage(t)
	repo2.Seed(&models.User{UID: "u2", MicrosoftAvatarSync: true})

	h.afterLink(context.Background(), "u1", oauth.ProviderIdentity{
		ProviderID: "ms-1",
		AvatarData: []byte("raw-avatar-bytes"),
		AvatarCT:   "image/png",
	})
	h2.afterLink(context.Background(), "u2", oauth.ProviderIdentity{
		ProviderID: "ms-2",
		AvatarURL:  "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("data-url-avatar")),
	})

	if !waitUntil(2*time.Second, func() bool { return len(storage.Uploaded) > 0 }) {
		t.Error("raw avatar data should be uploaded asynchronously after link")
	}
	if !waitUntil(2*time.Second, func() bool { return len(storage2.Uploaded) > 0 }) {
		t.Error("data URL avatar should be parsed and uploaded after link")
	}
}

// 哈希未变化时不重复转存
func TestAfterLoginSkipsWhenAvatarUnchanged(t *testing.T) {
	h, repo, storage := newHandlerWithStorage(t)

	data := []byte("avatar-v1")
	user := &models.User{UID: "u1", MicrosoftAvatarSync: true}
	user.MicrosoftAvatarHash = sql.NullString{String: h.calculateAvatarHash(data), Valid: true}
	repo.Seed(user)

	h.afterLogin(context.Background(), user, oauth.ProviderIdentity{AvatarData: data, AvatarCT: "image/png"})
	// 等待一段时间确认没有发生上传
	time.Sleep(150 * time.Millisecond)
	if len(storage.Uploaded) != 0 {
		t.Errorf("Uploaded = %v, want none when avatar hash unchanged", storage.Uploaded)
	}

	// 头像变化后应重新转存
	h.afterLogin(context.Background(), user, oauth.ProviderIdentity{AvatarData: []byte("avatar-v2"), AvatarCT: "image/png"})
	if !waitUntil(2*time.Second, func() bool { return len(storage.Uploaded) > 0 }) {
		t.Error("changed avatar should be re-uploaded")
	}
}

// 解绑：data URL 与空头像不触发删除；真实 URL 需要从存储中删除
func TestAfterUnlinkDeletesStoredAvatar(t *testing.T) {
	h, repo, storage := newHandlerWithStorage(t)

	// 无头像
	h.afterUnlink(context.Background(), "u1", &models.User{UID: "u1"})
	// data URL 头像（未落盘）
	h.afterUnlink(context.Background(), "u2", &models.User{UID: "u2", MicrosoftAvatarURL: sql.NullString{String: "data:image/png;base64,AAAA", Valid: true}})

	time.Sleep(150 * time.Millisecond)
	if len(storage.DeletedUsers) != 0 {
		t.Errorf("DeletedUsers = %v, want none for in-memory avatars", storage.DeletedUsers)
	}

	// 已落盘的真实 URL
	repo.Seed(&models.User{UID: "u3"})
	h.afterUnlink(context.Background(), "u3", &models.User{
		UID:                "u3",
		MicrosoftAvatarURL: sql.NullString{String: "https://cdn.test/u3.webp", Valid: true},
	})
	if !waitUntil(2*time.Second, func() bool { return len(storage.DeletedUsers) > 0 }) {
		t.Error("stored avatar should be deleted from storage after unlink")
	}
}

// 头像同步关闭时不应转存
func TestProcessAvatarAsyncRespectsSyncDisabled(t *testing.T) {
	h, repo, storage := newHandlerWithStorage(t)
	repo.Seed(&models.User{UID: "u1", MicrosoftAvatarSync: false})

	h.processAvatarAsync("u1", "", []byte("avatar"), "image/png")

	time.Sleep(150 * time.Millisecond)
	if len(storage.Uploaded) != 0 {
		t.Errorf("Uploaded = %v, want none when avatar sync disabled", storage.Uploaded)
	}
}
