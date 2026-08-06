package services

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestValidateRedirectURIScheme(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{"https valid", "https://app.example.com/callback", false},
		{"https with port", "https://example.com:8443/cb", false},
		{"https uppercase scheme", "HTTPS://example.com/cb", false},
		{"https no host", "https:///cb", true},
		{"https empty", "https://", true},
		{"http localhost", "http://localhost:3000/cb", false},
		{"http 127.0.0.1", "http://127.0.0.1/cb", false},
		{"http ipv6 loopback", "http://[::1]/cb", false},
		{"http remote host rejected", "http://example.com/cb", true},
		{"javascript rejected", "javascript:alert(1)", true},
		{"data rejected", "data:text/html,hello", true},
		{"file rejected", "file:///etc/passwd", true},
		{"blob rejected", "blob:https://x/y", true},
		{"no scheme", "example.com/cb", true},
		{"garbage", "not a uri:::", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRedirectURIScheme(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRedirectURIScheme(%q) error = %v, wantErr %v", tt.uri, err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrOAuthInvalidRedirect) {
				t.Errorf("expected ErrOAuthInvalidRedirect, got %v", err)
			}
		})
	}
}

func TestGenerateRandomHex(t *testing.T) {
	s := &OAuthService{}
	for _, length := range []int{16, 32, 64} {
		got, err := s.generateRandomHex(length)
		if err != nil {
			t.Fatalf("generateRandomHex(%d) error = %v", length, err)
		}
		// 参数为字节数，输出为 hex 编码（2 倍长度）
		if len(got) != length*2 {
			t.Errorf("generateRandomHex(%d) length = %d, want %d", length, len(got), length*2)
		}
		if _, err := hex.DecodeString(got); err != nil {
			t.Errorf("output %q is not valid hex: %v", got, err)
		}
	}
	// 唯一性
	a, _ := s.generateRandomHex(32)
	b, _ := s.generateRandomHex(32)
	if a == b {
		t.Error("two random hex values should differ")
	}
}
