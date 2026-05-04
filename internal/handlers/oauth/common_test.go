package oauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func resetGlobalStore() {
	stateMu.Lock()
	states = make(map[string]*State)
	stateIndex = make(map[string]int64)
	stateCounter = 0
	stateMu.Unlock()

	linkMu.Lock()
	pendingLinks = make(map[string]*PendingLink)
	pendingIndex = make(map[string]int64)
	pendingCounter = 0
	linkMu.Unlock()
}

// ====================  GenerateState ====================

func TestGenerateState_Length(t *testing.T) {
	s, err := GenerateState()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s) != 32 {
		t.Errorf("expected 32 hex chars, got %d", len(s))
	}
}

func TestGenerateState_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		s, err := GenerateState()
		if err != nil {
			t.Fatalf("unexpected error at %d: %v", i, err)
		}
		if seen[s] {
			t.Fatalf("duplicate state generated at iteration %d", i)
		}
		seen[s] = true
	}
}

// ====================  GenerateLinkToken ====================

func TestGenerateLinkToken_Length(t *testing.T) {
	tk, err := GenerateLinkToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tk) != 48 {
		t.Errorf("expected 48 hex chars, got %d", len(tk))
	}
}

func TestGenerateLinkToken_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		tk, err := GenerateLinkToken()
		if err != nil {
			t.Fatalf("unexpected error at %d: %v", i, err)
		}
		if seen[tk] {
			t.Fatalf("duplicate link token at %d", i)
		}
		seen[tk] = true
	}
}

// ====================  GenerateCodeVerifier ====================

func TestGenerateCodeVerifier_Length(t *testing.T) {
	v, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Errorf("expected 43-128 chars, got %d", len(v))
	}
}

// ====================  GenerateCodeChallenge ====================

func TestGenerateCodeChallenge_Deterministic(t *testing.T) {
	verifier := "test-verifier-string-for-pkce"
	c1 := GenerateCodeChallenge(verifier)
	c2 := GenerateCodeChallenge(verifier)
	if c1 != c2 {
		t.Errorf("same verifier should produce same challenge: %s vs %s", c1, c2)
	}
}

func TestGenerateCodeChallenge_DifferentVerifiers(t *testing.T) {
	c1 := GenerateCodeChallenge("verifier-a")
	c2 := GenerateCodeChallenge("verifier-b")
	if c1 == c2 {
		t.Error("different verifiers should produce different challenges")
	}
}

func TestGenerateCodeChallenge_IsBase64URL(t *testing.T) {
	c := GenerateCodeChallenge("some-verifier")
	for _, r := range c {
		if r == '+' || r == '/' || r == '=' {
			t.Errorf("challenge should be base64url-encoded (no +/=): got %q", c)
		}
	}
}

// ====================  State Storage ====================

func TestSaveState_InsertAndRetrieve(t *testing.T) {
	resetGlobalStore()
	d := &State{Timestamp: time.Now().UnixMilli(), Action: ActionLogin}
	SaveState("state-1", d)

	got, ok := GetState("state-1")
	if !ok {
		t.Fatal("expected state to exist")
	}
	if got.Action != ActionLogin {
		t.Errorf("expected Action=login, got %s", got.Action)
	}
}

func TestGetState_NotFound(t *testing.T) {
	resetGlobalStore()
	_, ok := GetState("nonexistent")
	if ok {
		t.Error("expected false for missing state")
	}
}

func TestDeleteState(t *testing.T) {
	resetGlobalStore()
	SaveState("state-1", &State{Timestamp: time.Now().UnixMilli()})
	DeleteState("state-1")
	_, ok := GetState("state-1")
	if ok {
		t.Error("expected state to be deleted")
	}
}

func TestDeleteState_NonExisting(t *testing.T) {
	resetGlobalStore()
	DeleteState("nonexistent")
}

func TestGetAndDeleteState_Existing(t *testing.T) {
	resetGlobalStore()
	d := &State{Timestamp: time.Now().UnixMilli(), Action: ActionLink}
	SaveState("state-1", d)

	got, ok := GetAndDeleteState("state-1")
	if !ok {
		t.Fatal("expected state to exist")
	}
	if got.Action != ActionLink {
		t.Errorf("expected Action=link, got %s", got.Action)
	}
	_, ok = GetState("state-1")
	if ok {
		t.Error("state should be deleted after GetAndDelete")
	}
}

func TestGetAndDeleteState_NotFound(t *testing.T) {
	resetGlobalStore()
	_, ok := GetAndDeleteState("nonexistent")
	if ok {
		t.Error("expected false for missing state")
	}
}

func TestSaveState_FIFOEviction(t *testing.T) {
	resetGlobalStore()
	for i := 0; i < maxStatesCapacity+10; i++ {
		key := GenerateStateForTest(t)
		SaveState(key, &State{Timestamp: time.Now().UnixMilli()})
	}

	stateMu.RLock()
	size := len(states)
	stateMu.RUnlock()

	if size > maxStatesCapacity {
		t.Errorf("state map exceeded capacity: %d > %d", size, maxStatesCapacity)
	}
}

// ====================  PendingLink Storage ====================

func TestSavePendingLink_InsertAndRetrieve(t *testing.T) {
	resetGlobalStore()
	d := &PendingLink{UserUID: "uid-1", DisplayName: "Test User", Timestamp: time.Now().UnixMilli()}
	SavePendingLink("token-1", d)

	got, ok := GetPendingLink("token-1")
	if !ok {
		t.Fatal("expected pending link to exist")
	}
	if got.UserUID != "uid-1" {
		t.Errorf("expected uid=uid-1, got %s", got.UserUID)
	}
}

func TestGetPendingLink_NotFound(t *testing.T) {
	resetGlobalStore()
	_, ok := GetPendingLink("nonexistent")
	if ok {
		t.Error("expected false for missing pending link")
	}
}

func TestDeletePendingLink(t *testing.T) {
	resetGlobalStore()
	SavePendingLink("token-1", &PendingLink{Timestamp: time.Now().UnixMilli()})
	DeletePendingLink("token-1")
	_, ok := GetPendingLink("token-1")
	if ok {
		t.Error("pending link should be deleted")
	}
}

func TestGetAndDeletePendingLink_Existing(t *testing.T) {
	resetGlobalStore()
	d := &PendingLink{UserUID: "uid-1", DisplayName: "User", Timestamp: time.Now().UnixMilli()}
	SavePendingLink("token-1", d)

	got, ok := GetAndDeletePendingLink("token-1")
	if !ok {
		t.Fatal("expected pending link to exist")
	}
	if got.DisplayName != "User" {
		t.Errorf("expected DisplayName=User, got %s", got.DisplayName)
	}
	_, ok = GetPendingLink("token-1")
	if ok {
		t.Error("pending link should be deleted after GetAndDelete")
	}
}

func TestSavePendingLink_FIFOEviction(t *testing.T) {
	resetGlobalStore()
	for i := 0; i < maxPendingLinksCapacity+10; i++ {
		token := GenerateLinkTokenForTest(t)
		SavePendingLink(token, &PendingLink{Timestamp: time.Now().UnixMilli()})
	}

	linkMu.RLock()
	size := len(pendingLinks)
	linkMu.RUnlock()

	if size > maxPendingLinksCapacity {
		t.Errorf("pendingLinks exceeded capacity: %d > %d", size, maxPendingLinksCapacity)
	}
}

// ====================  SetAuthCookie / Redirect helpers ====================

func TestSetAuthCookie_EmptyToken(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	SetAuthCookie(c, "")
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "token" {
			t.Error("should not set cookie for empty token")
		}
	}
}

func TestRedirectWithError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	RedirectWithError(c, "http://localhost", "/login", "test_error")
	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=test_error") {
		t.Errorf("expected error in redirect URL, got %s", loc)
	}
}

func TestRedirectWithSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	RedirectWithSuccess(c, "http://localhost", "/dashboard", "microsoft_linked")
	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "success=microsoft_linked") {
		t.Errorf("expected success in redirect URL, got %s", loc)
	}
}

// ====================  Cleanup ====================

func TestCleanupExpiredData_RemovesStale(t *testing.T) {
	resetGlobalStore()

	past := time.Now().Add(-20 * time.Minute).UnixMilli()
	SaveState("old-state", &State{Timestamp: past})
	SavePendingLink("old-link", &PendingLink{Timestamp: past})

	fresh := time.Now().UnixMilli()
	SaveState("fresh-state", &State{Timestamp: fresh})
	SavePendingLink("fresh-link", &PendingLink{Timestamp: fresh})

	cleanupExpiredData()

	_, ok := GetState("old-state")
	if ok {
		t.Error("expired state should be removed")
	}
	_, ok = GetPendingLink("old-link")
	if ok {
		t.Error("expired pending link should be removed")
	}
	_, ok = GetState("fresh-state")
	if !ok {
		t.Error("fresh state should remain")
	}
	_, ok = GetPendingLink("fresh-link")
	if !ok {
		t.Error("fresh pending link should remain")
	}
}

func TestCleanupExpiredData_HandlesNilEntries(t *testing.T) {
	resetGlobalStore()
	stateMu.Lock()
	states["nil-state"] = nil
	stateMu.Unlock()
	linkMu.Lock()
	pendingLinks["nil-link"] = nil
	linkMu.Unlock()

	cleanupExpiredData()

	_, ok := GetState("nil-state")
	if ok {
		t.Error("nil state should be removed")
	}
	_, ok = GetPendingLink("nil-link")
	if ok {
		t.Error("nil pending link should be removed")
	}
}

// ====================  FIFO Eviction ====================

func TestFindOldestKeys_FewerThanCount(t *testing.T) {
	idx := map[string]int64{"a": 10, "b": 5}
	keys := findOldestKeys(idx, 10)
	if len(keys) != 2 {
		t.Errorf("expected all 2 keys, got %d", len(keys))
	}
}

func TestFindOldestKeys_ExactCount(t *testing.T) {
	idx := map[string]int64{"a": 1, "b": 2, "c": 3}
	keys := findOldestKeys(idx, 2)
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestFindOldestKeys_Empty(t *testing.T) {
	keys := findOldestKeys(map[string]int64{}, 5)
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

func TestFindOldestKeys_LargeCount(t *testing.T) {
	idx := make(map[string]int64, 200)
	for i := 0; i < 200; i++ {
		idx[string(rune('a'+i%26))+string(rune('A'+i%26))+string(rune('0'+i%10))] = int64(i)
	}
	keys := findOldestKeys(idx, 50)
	if len(keys) != 50 {
		t.Errorf("expected 50 keys, got %d", len(keys))
	}
}

// ====================  Concurrent Access ====================

func TestConcurrent_SaveAndGetState(t *testing.T) {
	resetGlobalStore()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := string(rune(idx))
			SaveState(key, &State{Timestamp: time.Now().UnixMilli()})
			GetState(key)
		}(i)
	}
	wg.Wait()
}

func TestConcurrent_GetAndDeleteState(t *testing.T) {
	resetGlobalStore()
	for i := 0; i < 50; i++ {
		SaveState(string(rune(i)), &State{Timestamp: time.Now().UnixMilli()})
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			GetAndDeleteState(string(rune(idx % 25)))
		}(i)
	}
	wg.Wait()
}

func TestConcurrent_PendingLinkOperations(t *testing.T) {
	resetGlobalStore()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := string(rune(idx))
			SavePendingLink(key, &PendingLink{Timestamp: time.Now().UnixMilli(), UserUID: key})
			GetPendingLink(key)
		}(i)
	}
	wg.Wait()
}

// ====================  Helpers ====================

func GenerateStateForTest(t *testing.T) string {
	s, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState failed: %v", err)
	}
	return s
}

func GenerateLinkTokenForTest(t *testing.T) string {
	tk, err := GenerateLinkToken()
	if err != nil {
		t.Fatalf("GenerateLinkToken failed: %v", err)
	}
	return tk
}

// ====================  Benchmark ====================

func BenchmarkGenerateState(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = GenerateState()
	}
}

func BenchmarkGenerateCodeChallenge(b *testing.B) {
	v, _ := GenerateCodeVerifier()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateCodeChallenge(v)
	}
}
