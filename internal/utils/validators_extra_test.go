package utils

import (
	"net"
	"strings"
	"testing"
)

func TestValidateAvatarURLSpecialValues(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"only whitespace", "   ", false},
		// 微软/谷歌头像标记：合法且不走 URL 校验
		{"microsoft marker", "microsoft", true},
		{"google marker", "google", true},
		{"marker with padding", "  google  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateAvatarURL(tt.in)
			if got.Valid != tt.want {
				t.Errorf("ValidateAvatarURL(%q).Valid = %v, want %v (code %q)", tt.in, got.Valid, tt.want, got.ErrorCode)
			}
		})
	}
}

func TestValidateHTTPURLRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
		code string
	}{
		{"too long", "https://example.com/" + strings.Repeat("a", 2100), ErrURLTooLong},
		{"unparsable", "://not-a-url", ErrInvalidURL},
		{"javascript scheme", "javascript:alert(1)", ErrInvalidURLProtocol},
		{"data scheme", "data:image/png;base64,AAAA", ErrInvalidURLProtocol},
		{"ftp scheme", "ftp://example.com/a.png", ErrInvalidURLProtocol},
		{"empty hostname", "https:///a.png", ErrInvalidURL},
		{"localhost blocked", "https://localhost/a.png", ErrInvalidURL},
		{"private ip blocked", "https://192.168.1.10/a.png", ErrInvalidURL},
		{"loopback blocked", "https://127.0.0.1/a.png", ErrInvalidURL},
		// 用 IP 形式避免依赖 DNS：公网 IP 不被拦截，才会走到扩展名检查
		{"not an image", "https://8.8.8.8/page.html", ErrInvalidImageURL},
		{"image without extension", "https://8.8.8.8/avatar", ErrInvalidImageURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateHTTPURL(tt.in)
			if got.Valid {
				t.Fatalf("validateHTTPURL(%q) = valid, want invalid", tt.in)
			}
			if got.ErrorCode != tt.code {
				t.Errorf("errorCode = %q, want %q", got.ErrorCode, tt.code)
			}
		})
	}
}

func TestValidateHTTPURLAccepts(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"png over https", "https://8.8.8.8/avatar.png"},
		{"jpeg over http", "http://8.8.8.8/a.jpeg"},
		{"webp with query", "https://8.8.8.8/a.webp?v=1"},
		{"uppercase extension", "https://8.8.8.8/A.PNG"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateHTTPURL(tt.in)
			if !got.Valid {
				t.Errorf("validateHTTPURL(%q) = invalid (%q), want valid", tt.in, got.ErrorCode)
			}
		})
	}
}

// graph.microsoft.com 是特例：路径不是图片扩展名也放行（微软头像接口返回二进制）
func TestSpecialAllowedDomain(t *testing.T) {
	if !isSpecialAllowedDomain("graph.microsoft.com") {
		t.Error("graph.microsoft.com should be specially allowed")
	}
	if !isSpecialAllowedDomain("sub.graph.microsoft.com") {
		t.Error("subdomain of a special domain should also be allowed")
	}
	if isSpecialAllowedDomain("graph.microsoft.com.evil.com") {
		t.Error("lookalike domain must not be allowed")
	}
	if isSpecialAllowedDomain("example.com") {
		t.Error("unrelated domain should not be allowed")
	}
}

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"loopback", "127.0.0.1", true},
		{"private 10/8", "10.1.2.3", true},
		{"private 192.168/16", "192.168.0.1", true},
		{"unspecified", "0.0.0.0", true},
		{"link local unicast", "169.254.1.1", true},
		{"link local multicast", "224.0.0.1", true},
		{"public ip", "8.8.8.8", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("ParseIP(%q) failed", tt.ip)
			}
			if got := isBlockedIP(ip); got != tt.want {
				t.Errorf("isBlockedIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestHasImageExtension(t *testing.T) {
	tests := map[string]bool{
		"/a.png":  true,
		"/a.jpg":  true,
		"/a.jpeg": true,
		"/a.gif":  true,
		"/a.webp": true,
		"/a.bmp":  true,
		"/a.ico":  true,
		"/a.pngx": false,
		"/a.html": false,
		"":        false,
	}

	for path, want := range tests {
		if got := hasImageExtension(path); got != want {
			t.Errorf("hasImageExtension(%q) = %v, want %v", path, got, want)
		}
	}
}

// IsValid* 是 Validate* 的布尔包装，语义必须一致
func TestIsValidWrappers(t *testing.T) {
	if IsValidEmail("") || !IsValidEmail("alice@example.com") {
		t.Error("IsValidEmail semantics mismatch")
	}
	// 超长邮箱直接判否（RFC 最长 254）
	if IsValidEmail(strings.Repeat("a", 250) + "@example.com") {
		t.Error("over-long email should be invalid")
	}

	for _, username := range []string{"", "ab", "valid-name", "a", strings.Repeat("u", 100)} {
		if IsValidUsername(username) != ValidateUsername(username).Valid {
			t.Errorf("IsValidUsername(%q) disagrees with ValidateUsername", username)
		}
	}

	for _, password := range []string{"", "short", "Abcdef1!@#ghijklmn"} {
		if IsValidPassword(password) != ValidatePassword(password).Valid {
			t.Errorf("IsValidPassword(%q) disagrees with ValidatePassword", password)
		}
	}
}
