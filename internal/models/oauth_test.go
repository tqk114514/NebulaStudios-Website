package models

import (
	"errors"
	"testing"
	"time"
)

func TestOAuthClientValidate(t *testing.T) {
	tests := []struct {
		name    string
		client  *OAuthClient
		wantErr error
	}{
		{"nil client", nil, ErrOAuthClientRepoNilClient},
		{"empty client id", &OAuthClient{Name: "app", RedirectURI: "https://a.com/cb"}, ErrOAuthInvalidClientData},
		{"empty name", &OAuthClient{ClientID: "abc", RedirectURI: "https://a.com/cb"}, ErrOAuthInvalidClientData},
		{"empty redirect uri", &OAuthClient{ClientID: "abc", Name: "app"}, ErrOAuthInvalidClientData},
		{"valid", &OAuthClient{ClientID: "abc", Name: "app", RedirectURI: "https://a.com/cb"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.client.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want wrapping %v", err, tt.wantErr)
			}
		})
	}
}

func TestOAuthAuthCodeLifecycle(t *testing.T) {
	now := time.Now()
	expired := &OAuthAuthCode{ExpiresAt: now.Add(-time.Minute), Used: false}
	valid := &OAuthAuthCode{ExpiresAt: now.Add(time.Minute), Used: false}
	used := &OAuthAuthCode{ExpiresAt: now.Add(time.Minute), Used: true}

	if !expired.IsExpired() {
		t.Error("past ExpiresAt should be expired")
	}
	if valid.IsExpired() {
		t.Error("future ExpiresAt should not be expired")
	}
	if expired.IsValid() {
		t.Error("expired code should not be valid")
	}
	if used.IsValid() {
		t.Error("used code should not be valid")
	}
	if !valid.IsValid() {
		t.Error("unused unexpired code should be valid")
	}
	// fail-closed：零值 ExpiresAt（0001-01-01）应视为过期；nil 指针不报过期
	if !(&OAuthAuthCode{}).IsExpired() {
		t.Error("zero-value ExpiresAt should be treated as expired (fail-closed)")
	}
	if (*OAuthAuthCode)(nil).IsExpired() {
		t.Error("nil code should not report expired")
	}
}

func TestOAuthTokenExpiry(t *testing.T) {
	now := time.Now()

	if !(&OAuthAccessToken{ExpiresAt: now.Add(-time.Minute)}).IsExpired() {
		t.Error("expired access token should report expired")
	}
	if (&OAuthAccessToken{ExpiresAt: now.Add(time.Minute)}).IsExpired() {
		t.Error("unexpired access token should not report expired")
	}
	if !(&OAuthRefreshToken{ExpiresAt: now.Add(-time.Minute)}).IsExpired() {
		t.Error("expired refresh token should report expired")
	}
	if (&OAuthRefreshToken{ExpiresAt: now.Add(time.Minute)}).IsExpired() {
		t.Error("unexpired refresh token should not report expired")
	}
}
