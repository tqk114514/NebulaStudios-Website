package config

import (
	"errors"
	"strings"
	"testing"
)

// validConfig 返回一份能通过必需项校验的最小配置，供各规则测试改写
func validConfig() *Config {
	return &Config{
		DatabaseURL:           "postgres://user:pass@localhost:5432/db",
		JWTPrivateKey:         "-----BEGIN EC PRIVATE KEY-----",
		EmailWhitelistDomains: "example.com",
		captchaEnabledSet:     true,
		CaptchaEnabled:        false,
		PprofAddr:             "127.0.0.1:6060",
	}
}

func TestValidateConfig_PprofAddr(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr bool
	}{
		{
			name:    "disabled keeps non-loopback addr unvalidated",
			mutate:  func(c *Config) { c.PprofEnabled = false; c.PprofAddr = "0.0.0.0:6060" },
			wantErr: false,
		},
		{
			name:    "enabled with loopback addr",
			mutate:  func(c *Config) { c.PprofEnabled = true; c.PprofAddr = "127.0.0.1:6060" },
			wantErr: false,
		},
		{
			name:    "enabled with all-interfaces addr is rejected",
			mutate:  func(c *Config) { c.PprofEnabled = true; c.PprofAddr = ":6060" },
			wantErr: true,
		},
		{
			name:    "enabled with public addr is rejected",
			mutate:  func(c *Config) { c.PprofEnabled = true; c.PprofAddr = "203.0.113.10:6060" },
			wantErr: true,
		},
		{
			name: "enabled with public addr allowed by PPROF_ALLOW_REMOTE",
			mutate: func(c *Config) {
				c.PprofEnabled = true
				c.PprofAddr = "203.0.113.10:6060"
				c.PprofAllowRemote = true
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)

			err := validateConfig(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("validateConfig returned nil, want error")
				}
				if !errors.Is(err, ErrInvalidValue) {
					t.Fatalf("error = %v, want ErrInvalidValue", err)
				}
				if !strings.Contains(err.Error(), "loopback") {
					t.Fatalf("error %q should explain the loopback requirement", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("validateConfig returned %v, want nil", err)
			}
		})
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"ipv4 loopback", "127.0.0.1:6060", true},
		{"ipv4 loopback other port", "127.0.0.2:6060", true},
		{"localhost", "localhost:6060", true},
		{"ipv6 loopback", "[::1]:6060", true},
		{"all interfaces", ":6060", false},
		{"explicit any", "0.0.0.0:6060", false},
		{"public ip", "203.0.113.10:6060", false},
		{"missing port", "127.0.0.1", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLoopbackAddr(tt.addr); got != tt.want {
				t.Fatalf("isLoopbackAddr(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}
