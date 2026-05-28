package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"auth-system/internal/utils"

	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	defaultExportTokenCapacity = 1000
	defaultExportTokenTTL      = 5 * time.Minute
	exportTokenCleanupInterval = 5 * time.Minute
)

type exportTokenEntry struct {
	UserUID   string
	ExpiresAt time.Time
}

type ExportTokenService struct {
	cache   *lru.Cache[string, *exportTokenEntry]
	stopCh  chan struct{}
	stopOnce sync.Once
}

func NewExportTokenService() (*ExportTokenService, error) {
	cache, err := lru.New[string, *exportTokenEntry](defaultExportTokenCapacity)
	if err != nil {
		return nil, fmt.Errorf("failed to create export token cache: %w", err)
	}

	svc := &ExportTokenService{
		cache:  cache,
		stopCh: make(chan struct{}),
	}

	go svc.cleanupLoop()

	return svc, nil
}

func (s *ExportTokenService) Generate(userUID string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	s.cache.Add(token, &exportTokenEntry{
		UserUID:   userUID,
		ExpiresAt: time.Now().Add(defaultExportTokenTTL),
	})

	utils.LogInfo("EXPORT_TOKEN", fmt.Sprintf("Token generated: userUID=%s", userUID))
	return token, nil
}

func (s *ExportTokenService) ValidateAndConsume(token string) (string, bool) {
	entry, ok := s.cache.Get(token)
	if !ok {
		return "", false
	}

	if time.Now().After(entry.ExpiresAt) {
		s.cache.Remove(token)
		return "", false
	}

	s.cache.Remove(token)
	return entry.UserUID, true
}

func (s *ExportTokenService) cleanupLoop() {
	ticker := time.NewTicker(exportTokenCleanupInterval)
	defer ticker.Stop()

	utils.LogInfo("EXPORT_TOKEN", "Cleanup loop started")

	for {
		select {
		case <-ticker.C:
			s.cleanupExpired()
		case <-s.stopCh:
			utils.LogInfo("EXPORT_TOKEN", "Cleanup loop stopped")
			return
		}
	}
}

func (s *ExportTokenService) cleanupExpired() {
	now := time.Now()
	count := 0
	for _, key := range s.cache.Keys() {
		entry, ok := s.cache.Get(key)
		if !ok {
			continue
		}
		if now.After(entry.ExpiresAt) {
			s.cache.Remove(key)
			count++
		}
	}
	if count > 0 {
		utils.LogInfo("EXPORT_TOKEN", fmt.Sprintf("Cleanup completed: expired=%d", count))
	}
}

func (s *ExportTokenService) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}