package main

import "testing"

func TestReleaseChannelOrdinal(t *testing.T) {
	cases := []struct {
		have, want string
		atLeast    bool
	}{
		{"0.1.0-rc11q", "0.1.0-rc11p", true},
		{"0.1.0-rc11p", "0.1.0-rc11q", false},
		{"0.1.0-rc11p", "0.1.0-rc11p", true},
		{"0.1.0-rc12a", "0.1.0-rc11z", true},
	}
	for _, c := range cases {
		got := versionAtLeast(c.have, c.want)
		if got != c.atLeast {
			t.Fatalf("versionAtLeast(%q,%q)=%v want %v", c.have, c.want, got, c.atLeast)
		}
	}
}

func TestFilterActiveMinerNotices(t *testing.T) {
	doc := minerNoticesDoc{
		Notices: []minerNotice{
			{ID: "a", Title: "old", RecommendedVersion: "0.1.0-rc11p"},
			{ID: "b", Title: "new", RecommendedVersion: "0.1.0-rc11q"},
			{ID: "c", Title: "expired", ExpiresUnix: 1},
		},
	}
	active := filterActiveMinerNotices(doc, "0.1.0-rc11p")
	if len(active) != 1 || active[0].ID != "b" {
		t.Fatalf("active=%+v", active)
	}
	active2 := filterActiveMinerNotices(doc, "0.1.0-rc11q")
	if len(active2) != 0 {
		t.Fatalf("expected none when up to date, got %+v", active2)
	}
}
