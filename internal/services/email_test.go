package services

import (
	"testing"

	"auth-system/internal/config"

	"github.com/wneessen/go-mail"
)

// TestSetSenderHeaders 验证发件人身份相关的头：
// 配置了 SMTP_FROM_NAME 时 From 写为 `"显示名" <地址>`；未配置或显示名会破坏
// RFC 5322 quoted-string 时 From 降级为纯地址，避免装饰性字段阻断验证码投递。
// 两种情况都断言不写 User-Agent/X-Mailer：收件方客户端会把该字段渲染成发件人旁的
// "使用 XXX" 标注，go-mail 的默认值还会外泄依赖库名与版本号
func TestSetSenderHeaders(t *testing.T) {
	const addr = "noreply@nebulastudios.top"

	cases := []struct {
		fromName string
		wantName string
		wantAddr string
	}{
		{"Nebula Studios", "Nebula Studios", addr},
		{"", "", addr},
		{`evil" <x@evil.example>`, "", addr},
		{`back\slash`, "", addr},
	}

	for _, tc := range cases {
		s := &EmailService{cfg: &config.Config{SMTPFrom: addr, SMTPFromName: tc.fromName}}

		msg := mail.NewMsg(mail.WithNoDefaultUserAgent())
		if err := s.setSenderHeaders(msg); err != nil {
			t.Fatalf("SMTP_FROM_NAME=%q: setSenderHeaders 返回错误 %v", tc.fromName, err)
		}

		from := msg.GetFrom()
		if len(from) != 1 {
			t.Fatalf("SMTP_FROM_NAME=%q: From 地址数 = %d，期望 1", tc.fromName, len(from))
		}
		if from[0].Name != tc.wantName {
			t.Errorf("SMTP_FROM_NAME=%q: 显示名 = %q，期望 %q", tc.fromName, from[0].Name, tc.wantName)
		}
		if from[0].Address != tc.wantAddr {
			t.Errorf("SMTP_FROM_NAME=%q: 地址 = %q，期望 %q", tc.fromName, from[0].Address, tc.wantAddr)
		}

		for _, header := range []mail.Header{mail.HeaderUserAgent, mail.HeaderXMailer} {
			if got := msg.GetGenHeader(header); len(got) != 0 {
				t.Errorf("SMTP_FROM_NAME=%q: 不应设置 %s，实际 %q", tc.fromName, header, got)
			}
		}
	}
}
