package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersionPrefersBuildFlagThenRevision(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef0123"}, {Key: "vcs.modified", Value: "true"}}}
	for _, test := range []struct {
		built string
		info  *debug.BuildInfo
		ok    bool
		want  string
	}{
		{"v0.1.0", info, true, "v0.1.0"},
		{"", info, true, "dev (0123456789ab-dirty)"},
		{"", &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}}, true, "dev (abc)"},
		{"", &debug.BuildInfo{}, true, "dev"},
		{"", nil, false, "dev"},
	} {
		if got := resolveVersion(test.built, test.info, test.ok); got != test.want {
			t.Errorf("resolveVersion(%q) = %q, want %q", test.built, got, test.want)
		}
	}
}
