package models

import "testing"

// 导入路径上的头像哨兵校验：备份里存在"哨兵但 Provider 头像为空"的历史行
func TestAvatarForImport(t *testing.T) {
	const def = "https://cdn.test/default.svg"

	cases := []struct {
		name   string
		avatar string
		ms     string
		google string
		want   string
	}{
		{"microsoft 哨兵且头像已落库则保留", "microsoft", "https://cdn.test/ms.webp", "", "microsoft"},
		{"google 哨兵且头像已落库则保留", "google", "", "https://cdn.test/g.webp", "google"},
		{"microsoft 哨兵无头像则回落默认", "microsoft", "", "", def},
		{"google 哨兵无头像则回落默认", "google", "", "", def},
		{"自定义地址原样保留", "https://img.example/a.png", "", "", "https://img.example/a.png"},
		{"无头像原样保留", "", "", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := avatarForImport(tc.avatar, tc.ms, tc.google, def); got != tc.want {
				t.Errorf("avatarForImport(%q,%q,%q) = %q, want %q", tc.avatar, tc.ms, tc.google, got, tc.want)
			}
		})
	}
}
