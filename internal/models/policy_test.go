package models

import "testing"

// testManifest 构造三类型政策的测试清单
func testManifest() PolicyManifest {
	return PolicyManifest{
		"privacy": map[string]PolicyVersionMeta{
			"v1.md": {UpdateDate: "2026-01-01", EffectiveDate: "2026-02-01"},
			"v2.md": {UpdateDate: "2026-03-01", EffectiveDate: "2026-04-01"},
		},
		"terms": map[string]PolicyVersionMeta{
			"v1.md": {UpdateDate: "2026-01-15", EffectiveDate: "2026-01-20"},
		},
		"cookie": map[string]PolicyVersionMeta{
			"v1.md": {UpdateDate: "2026-05-01", EffectiveDate: "2026-06-01"},
		},
	}
}

func TestGetLatestEffectiveVersion(t *testing.T) {
	m := testManifest()

	// 2026-03-15：privacy 已生效的是 v1（v2 4 月生效）
	if v := m.GetLatestEffectiveVersion("privacy", "2026-03-15"); v != "v1" {
		t.Errorf("GetLatestEffectiveVersion(privacy, 2026-03-15) = %q, want v1", v)
	}
	// 2026-04-15：v2 已生效
	if v := m.GetLatestEffectiveVersion("privacy", "2026-04-15"); v != "v2" {
		t.Errorf("GetLatestEffectiveVersion(privacy, 2026-04-15) = %q, want v2", v)
	}
	// 生效日当天算生效
	if v := m.GetLatestEffectiveVersion("terms", "2026-01-20"); v != "v1" {
		t.Errorf("GetLatestEffectiveVersion(terms, 2026-01-20) = %q, want v1 (当天生效)", v)
	}
	// 生效前为空
	if v := m.GetLatestEffectiveVersion("cookie", "2026-05-31"); v != "" {
		t.Errorf("GetLatestEffectiveVersion(cookie, 2026-05-31) = %q, want empty", v)
	}
	// 未知类型为空
	if v := m.GetLatestEffectiveVersion("nonexistent", "2026-06-01"); v != "" {
		t.Errorf("unknown policy type = %q, want empty", v)
	}
}

func TestGetPublicNoticeVersions(t *testing.T) {
	m := testManifest()

	// 2026-03-15：privacy 的 v2 在公示期（03-01 <= 03-15 < 04-01）
	notices := m.GetPublicNoticeVersions("2026-03-15")
	if len(notices) != 1 {
		t.Fatalf("GetPublicNoticeVersions(2026-03-15) = %d entries, want 1", len(notices))
	}
	if notices[0].PolicyType != "privacy" || notices[0].Version != "v2" {
		t.Errorf("notice = %+v, want privacy/v2", notices[0])
	}

	// 2026-05-15：cookie v1 在公示期（05-01 <= 05-15 < 06-01）
	notices = m.GetPublicNoticeVersions("2026-05-15")
	if len(notices) != 1 || notices[0].PolicyType != "cookie" {
		t.Errorf("GetPublicNoticeVersions(2026-05-15) = %v, want 1 (cookie v1 公示中)", notices)
	}

	// 2026-02-15：privacy/terms 均已生效、cookie 未开始 → 无公示期
	if notices := m.GetPublicNoticeVersions("2026-02-15"); len(notices) != 0 {
		t.Errorf("GetPublicNoticeVersions(2026-02-15) = %v, want empty", notices)
	}

	// 边界端点：半开区间 [update_date, effective_date)
	// now == update_date：公示开始当天算公示
	if notices := m.GetPublicNoticeVersions("2026-05-01"); len(notices) != 1 || notices[0].PolicyType != "cookie" {
		t.Errorf("GetPublicNoticeVersions(2026-05-01) = %v, want cookie (update_date 当天公示开始)", notices)
	}
	// now == effective_date：生效当天已不在公示（转为已生效）
	if notices := m.GetPublicNoticeVersions("2026-06-01"); len(notices) != 0 {
		t.Errorf("GetPublicNoticeVersions(2026-06-01) = %v, want empty (effective_date 当天已生效)", notices)
	}
}
