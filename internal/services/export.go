package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const (
	otacLength   = 16
	otacTTL      = 5 * time.Minute
	otacMaxTries = 3

	fileTokenLength = 32
	fileTokenTTL    = 5 * time.Minute
)

// otacEntry OTAC 条目
type otacEntry struct {
	Code      string
	RequestID string
	UserUID   string // 绑定生成 OTAC 的用户，ValidateOTAC 时校验调用者身份
	CreatedAt time.Time
	Attempts  int
}

// fileTokenEntry 临时文件 token 条目
type fileTokenEntry struct {
	Data      []byte
	Filename  string
	CreatedAt time.Time
}

// ExportService 导出/导入服务
type ExportService struct {
	mu           sync.Mutex
	currentOTAC  *otacEntry
	fileTokenMap map[string]*fileTokenEntry
}

// NewExportService 创建导出服务
func NewExportService() *ExportService {
	svc := &ExportService{
		fileTokenMap: make(map[string]*fileTokenEntry),
	}
	go svc.cleanupLoop()
	return svc
}

// GenerateOTAC 生成新的 OTAC（旧 OTAC 立即失效），绑定生成者 userUID
func (s *ExportService) GenerateOTAC(userUID string) (requestID, code string, expiresAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	codeBytes := make([]byte, otacLength/2)
	if _, err := rand.Read(codeBytes); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	code = hex.EncodeToString(codeBytes)

	requestIDBytes := make([]byte, 8)
	if _, err := rand.Read(requestIDBytes); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	requestID = hex.EncodeToString(requestIDBytes)

	now := time.Now()
	s.currentOTAC = &otacEntry{
		Code:      code,
		RequestID: requestID,
		UserUID:   userUID,
		CreatedAt: now,
		Attempts:  0,
	}

	return requestID, code, now.Add(otacTTL)
}

// ValidateOTAC 验证 OTAC，成功则销毁。校验调用者 userUID 必须与生成者一致。
func (s *ExportService) ValidateOTAC(requestID, code, userUID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentOTAC == nil {
		return fmt.Errorf("no active OTAC")
	}

	if s.currentOTAC.RequestID != requestID {
		return fmt.Errorf("request ID mismatch")
	}

	if s.currentOTAC.UserUID != userUID {
		return fmt.Errorf("user mismatch")
	}

	if time.Since(s.currentOTAC.CreatedAt) > otacTTL {
		s.currentOTAC = nil
		return fmt.Errorf("OTAC expired")
	}

	s.currentOTAC.Attempts++

	if s.currentOTAC.Code != code {
		if s.currentOTAC.Attempts >= otacMaxTries {
			s.currentOTAC = nil
			return fmt.Errorf("OTAC invalidated after %d failed attempts", otacMaxTries)
		}
		return fmt.Errorf("OTAC mismatch (attempt %d/%d)", s.currentOTAC.Attempts, otacMaxTries)
	}

	s.currentOTAC = nil
	return nil
}

// RevokeOTAC 主动撤销当前 OTAC
func (s *ExportService) RevokeOTAC() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentOTAC = nil
}

// StoreFile 暂存文件并返回 token
func (s *ExportService) StoreFile(data []byte, filename string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokenBytes := make([]byte, fileTokenLength/2)
	if _, err := rand.Read(tokenBytes); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	token := hex.EncodeToString(tokenBytes)

	s.fileTokenMap[token] = &fileTokenEntry{
		Data:      data,
		Filename:  filename,
		CreatedAt: time.Now(),
	}

	return token
}

// RetrieveFile 根据 token 取出暂存文件
func (s *ExportService) RetrieveFile(token string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.fileTokenMap[token]
	if !ok {
		return nil, "", fmt.Errorf("file token not found or expired")
	}

	delete(s.fileTokenMap, token)
	return entry.Data, entry.Filename, nil
}

// cleanupLoop 定期清理过期的 OTAC 和文件 token
func (s *ExportService) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()

		if s.currentOTAC != nil && time.Since(s.currentOTAC.CreatedAt) > otacTTL {
			s.currentOTAC = nil
		}

		for token, entry := range s.fileTokenMap {
			if time.Since(entry.CreatedAt) > fileTokenTTL {
				delete(s.fileTokenMap, token)
			}
		}

		s.mu.Unlock()
	}
}
