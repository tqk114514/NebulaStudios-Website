package utils

import (
	"strings"
	"testing"
)

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"no-at-sign", "***"}, // 不是邮箱
		{"alice@example.com", "a***@e***.com"},
		{"a@b", "a***@***"}, // 域名无点
		{"bob@mail.example.com", "b***@m***.com"},
	}

	for _, tt := range tests {
		if got := maskEmail(tt.in); got != tt.want {
			t.Errorf("maskEmail(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMaskIPv4(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"192.168.1.100", "192.168.***.***"},
		{"10.0.0.1", "10.0.***.***"},
		{"1.2.3", "***.***.***"}, // 段数不对
	}

	for _, tt := range tests {
		if got := maskIPv4(tt.in); got != tt.want {
			t.Errorf("maskIPv4(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMaskIPv6(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"too short", "abcd", "****:****"},
		{"all zeros", "::", "0:0:****:****:****:****:****:****"},
		{"leading double colon", "::1", "0:0:****:****:****:****:****:****"},
		{"trailing double colon", "1::", "1:0:****:****:****:****:****:****"},
		{"middle double colon", "2001:db8::1", "2001:db8:****:****:****:****:****:****:****"},
		{"full form", "2001:0db8:85a3:0000:0000:8a2e:0370:7334", "2001:0db8:****:****:****:****:****:****"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskIPv6(tt.in); got != tt.want {
				t.Errorf("maskIPv6(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMaskToken(t *testing.T) {
	// 无分隔符：原样返回
	if got := maskToken("noseparator"); got != "noseparator" {
		t.Errorf("maskToken without separator = %q, want unchanged", got)
	}
	if got := maskToken(""); got != "" {
		t.Errorf("maskToken(\"\") = %q, want empty", got)
	}

	long := strings.Repeat("a", 40)
	got := maskToken("token=" + long)
	if !strings.HasPrefix(got, "token=aaaa***[MASKED]") {
		t.Errorf("maskToken(long) = %q, want prefix token=aaaa***[MASKED]", got)
	}

	// 短值不保留前缀，避免泄露过长内容
	got = maskToken("token=short")
	if got != "token=***[MASKED]" {
		t.Errorf("maskToken(short) = %q, want token=***[MASKED]", got)
	}
}

// 脱敏是"防泄漏"的最后一道闸：原始值必须不出现在日志文本里
func TestMaskSensitiveData(t *testing.T) {
	message := "user alice@example.com from 192.168.1.100 with token=" + strings.Repeat("b", 40)
	masked := maskSensitiveData(message)

	for _, leak := range []string{"alice@example.com", "192.168.1.100", strings.Repeat("b", 40)} {
		if strings.Contains(masked, leak) {
			t.Errorf("masked = %q, still contains %q", masked, leak)
		}
	}
	if !strings.Contains(masked, "a***@e***.com") || !strings.Contains(masked, "192.168.***.***") {
		t.Errorf("masked = %q, want masked email and ip", masked)
	}

	// 普通文本不应被误伤
	plain := "user logged in successfully"
	if got := maskSensitiveData(plain); got != plain {
		t.Errorf("maskSensitiveData(%q) = %q, want unchanged", plain, got)
	}
}

func TestNormalizeKV(t *testing.T) {
	// 偶数：原样返回
	msg, kv := normalizeKV("m", []any{"a", 1, "b", 2})
	if msg != "m" || len(kv) != 4 {
		t.Errorf("even case: msg = %q, kv = %v", msg, kv)
	}

	// 奇数：末尾裸字符串并入 message
	msg, kv = normalizeKV("m", []any{"a", 1, "uid=abc"})
	if msg != "m: uid=abc" {
		t.Errorf("odd case msg = %q, want \"m: uid=abc\"", msg)
	}
	if len(kv) != 2 {
		t.Errorf("odd case kv = %v, want 2 entries", kv)
	}

	// 奇数但末尾为空串：不应产生多余冒号
	msg, _ = normalizeKV("m", []any{"a", 1, ""})
	if msg != "m" {
		t.Errorf("empty tail msg = %q, want m", msg)
	}
}

func TestMaskFields(t *testing.T) {
	// 值位置（奇数下标）的字符串才会被脱敏
	kv := []any{"email", "alice@example.com", "count", 3}
	masked := maskFields(kv)
	if masked[1] != "a***@e***.com" {
		t.Errorf("masked email = %v, want a***@e***.com", masked[1])
	}
	if masked[3] != 3 {
		t.Errorf("non-string value = %v, want unchanged", masked[3])
	}

	// 奇数长度不应越界
	if got := maskFields([]any{"only-key"}); len(got) != 1 {
		t.Errorf("maskFields with single element len = %d, want 1", len(got))
	}
}

// 允许替换全局 Logger（测试/自定义实现），替换后 GetLogger 应返回同一个实例
func TestSetAndGetLogger(t *testing.T) {
	original := GetLogger()
	t.Cleanup(func() { SetLogger(original) })

	custom := &zapLogger{}
	SetLogger(custom)

	if GetLogger() != Logger(custom) {
		t.Error("GetLogger should return the logger set via SetLogger")
	}
}

func TestLogAndSyncDoNotPanic(t *testing.T) {
	Log("plain log message without sensitive data")
	Log("contact alice@example.com")
	SyncLogger()
}

func TestAppendFieldsIncludesCategory(t *testing.T) {
	fields := appendFields("AUTH", []any{"request_id", "req-1"}, []any{"uid", "u1"})
	joined := make([]string, 0, len(fields))
	for _, f := range fields {
		joined = append(joined, stringOf(f))
	}

	if len(joined) != 6 || joined[0] != "category" || joined[1] != "AUTH" {
		t.Errorf("appendFields = %v, want category first", joined)
	}
	if !containsAll(joined, "request_id", "req-1", "uid", "u1") {
		t.Errorf("appendFields = %v, want existing and new fields", joined)
	}
}

func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "<non-string>"
}

func containsAll(haystack []string, needles ...string) bool {
	for _, needle := range needles {
		found := false
		for _, item := range haystack {
			if item == needle {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
