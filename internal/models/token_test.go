package models

import (
	"testing"
	"time"
)

func TestTokenIsExpired(t *testing.T) {
	now := time.Now().UnixMilli()
	tests := []struct {
		name  string
		token *Token
		want  bool
	}{
		{"nil token", nil, false},
		{"expired", &Token{ExpireTime: now - 1000}, true},
		{"not expired", &Token{ExpireTime: now + 60000}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsExpired(); got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokenIsUsed(t *testing.T) {
	tests := []struct {
		name  string
		token *Token
		want  bool
	}{
		{"nil token", nil, false},
		{"used", &Token{Used: tokenUsed}, true},
		{"unused", &Token{Used: 0}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsUsed(); got != tt.want {
				t.Errorf("IsUsed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCodeIsExpired(t *testing.T) {
	now := time.Now().UnixMilli()
	tests := []struct {
		name string
		code *Code
		want bool
	}{
		{"nil code", nil, false},
		{"expired", &Code{ExpireTime: now - 1000}, true},
		{"not expired", &Code{ExpireTime: now + 60000}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.code.IsExpired(); got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCodeIsVerified(t *testing.T) {
	tests := []struct {
		name string
		code *Code
		want bool
	}{
		{"nil code", nil, false},
		{"verified", &Code{Verified: codeVerified}, true},
		{"unverified", &Code{Verified: 0}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.code.IsVerified(); got != tt.want {
				t.Errorf("IsVerified() = %v, want %v", got, tt.want)
			}
		})
	}
}
