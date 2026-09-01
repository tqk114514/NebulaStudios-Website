package utils

import (
	"strings"
	"testing"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
		wantCode  string
	}{
		{"empty", "", false, ErrInvalidEmail},
		{"whitespace only", "   ", false, ErrInvalidEmail},
		{"no at sign", "plainaddress", false, ErrInvalidEmail},
		{"no domain dot", "user@localhost", false, ErrInvalidEmail},
		{"double at", "a@b@c.com", false, ErrInvalidEmail},
		{"empty local", "@example.com", false, ErrInvalidEmail},
		{"too long", strings.Repeat("a", 250) + "@example.com", false, ErrInvalidEmail},
		{"valid basic", "user@example.com", true, ""},
		{"valid with plus", "user+tag@example.co.uk", true, ""},
		{"valid uppercase normalized", "USER@Example.COM", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateEmail(tt.input)
			if got.Valid != tt.wantValid {
				t.Errorf("ValidateEmail(%q).Valid = %v, want %v", tt.input, got.Valid, tt.wantValid)
			}
			if got.ErrorCode != tt.wantCode {
				t.Errorf("ValidateEmail(%q).ErrorCode = %q, want %q", tt.input, got.ErrorCode, tt.wantCode)
			}
			if tt.wantValid && got.Value != strings.ToLower(strings.TrimSpace(tt.input)) {
				t.Errorf("ValidateEmail(%q).Value = %q, want normalized", tt.input, got.Value)
			}
		})
	}
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
		wantCode  string
	}{
		{"empty", "", false, ErrInvalidUsername},
		{"whitespace only", "   ", false, ErrInvalidUsername},
		{"too long 16 runes", "一二三四五六七八九十一二三四五六", false, ErrUsernameTooLong},
		{"valid single", "a", true, ""},
		{"valid 15 runes", "一二三四五六七八九十12345", true, ""},
		{"valid with spaces trimmed", "  alice  ", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateUsername(tt.input)
			if got.Valid != tt.wantValid {
				t.Errorf("ValidateUsername(%q).Valid = %v, want %v", tt.input, got.Valid, tt.wantValid)
			}
			if got.ErrorCode != tt.wantCode {
				t.Errorf("ValidateUsername(%q).ErrorCode = %q, want %q", tt.input, got.ErrorCode, tt.wantCode)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	valid := "Abcdef1!@#ghijklmn" // 16 chars, digit, special, upper, lower
	tests := []struct {
		name      string
		input     string
		wantValid bool
		wantCode  string
	}{
		{"empty", "", false, ErrInvalidPassword},
		{"too short 15", "Abcdef1!@#ghijk", false, ErrPasswordTooShort},
		{"too long 65", valid + strings.Repeat("a", 49), false, ErrPasswordTooLong},
		{"no digit", "Abcdefgh!@#ijklmn", false, ErrPasswordNoNumber},
		{"no special", "Abcdefgh1ijklmn0", false, ErrPasswordNoSpecial},
		{"no upper", "abcdefgh1!@#ijklmn", false, ErrPasswordNoCase},
		{"no lower", "ABCDEFGH1!@#IJKLMN", false, ErrPasswordNoCase},
		{"valid", valid, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidatePassword(tt.input)
			if got.Valid != tt.wantValid {
				t.Errorf("ValidatePassword(%q).Valid = %v, want %v", tt.input, got.Valid, tt.wantValid)
			}
			if got.ErrorCode != tt.wantCode {
				t.Errorf("ValidatePassword(%q).ErrorCode = %q, want %q", tt.input, got.ErrorCode, tt.wantCode)
			}
		})
	}
}

func TestValidateCode(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
	}{
		{"empty", "", false},
		{"short", "12345", false},
		{"long", "1234567", false},
		{"contains zero", "123450", false},
		{"contains capital O", "12345O", false},
		{"contains l", "12345l", false},
		{"contains I", "12345I", false},
		{"valid", "A1b2C3", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateCode(tt.input)
			if got.Valid != tt.wantValid {
				t.Errorf("ValidateCode(%q).Valid = %v, want %v", tt.input, got.Valid, tt.wantValid)
			}
		})
	}
}

func TestValidateAvatarURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
		wantCode  string
	}{
		{"empty", "", false, ErrInvalidURL},
		{"special microsoft", "microsoft", true, ""},
		{"special google", "google", true, ""},
		{"data url rejected (custom avatar stores URL only, no inline image)", "data:image/png;base64,iVBORw0KGgoAAAANSUhEUg==", false, ErrInvalidURLProtocol},
		{"loopback ip blocked", "https://127.0.0.1/x.png", false, ErrInvalidURL},
		{"private ip blocked", "https://10.0.0.1/x.png", false, ErrInvalidURL},
		{"link local blocked", "https://169.254.1.1/x.png", false, ErrInvalidURL},
		{"non-image extension", "https://8.8.8.8/photo.jpg.html", false, ErrInvalidImageURL},
		{"no extension", "https://8.8.8.8/avatar", false, ErrInvalidImageURL},
		{"ftp protocol", "ftp://example.com/a.jpg", false, ErrInvalidURLProtocol},
		// 注：合法 http/https URL 需经过 isBlockedHost 的 DNS 解析检查（防 SSRF），
		// 依赖外部 DNS 的用例不稳定，故不在此测试；非法用例用公网字面 IP（ParseIP 分支不查 DNS）
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateAvatarURL(tt.input)
			if got.Valid != tt.wantValid {
				t.Errorf("ValidateAvatarURL(%q).Valid = %v, want %v", tt.input, got.Valid, tt.wantValid)
			}
			if got.ErrorCode != tt.wantCode {
				t.Errorf("ValidateAvatarURL(%q).ErrorCode = %q, want %q", tt.input, got.ErrorCode, tt.wantCode)
			}
		})
	}
}

func TestIsValidPassword(t *testing.T) {
	if !IsValidPassword("Abcdef1!@#ghijklmn") {
		t.Error("expected valid password to pass")
	}
	if IsValidPassword("short") {
		t.Error("expected short password to fail")
	}
	if IsValidPassword("") {
		t.Error("expected empty password to fail")
	}
	if IsValidPassword("abcdefgh1!@#ijklmn") { // no upper
		t.Error("expected password without uppercase to fail")
	}
}
