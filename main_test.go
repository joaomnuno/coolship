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

func TestColorEnabledNeedsTerminalAndNoOptOut(t *testing.T) {
	for _, test := range []struct {
		name     string
		terminal bool
		env      map[string]string
		args     []string
		want     bool
	}{
		{"terminal", true, nil, []string{"status"}, true},
		{"pipe", false, nil, nil, false},
		{"NO_COLOR", true, map[string]string{"NO_COLOR": "1"}, nil, false},
		{"NO_COLOR empty is unset", true, map[string]string{"NO_COLOR": ""}, nil, true},
		{"TERM=dumb", true, map[string]string{"TERM": "dumb"}, nil, false},
		{"TERM=xterm", true, map[string]string{"TERM": "xterm-256color"}, nil, true},
		{"CI", true, map[string]string{"CI": "true"}, nil, false},
		{"--no-color", true, nil, []string{"deploy", "--no-color"}, false},
		{"--no-color=true", true, nil, []string{"--no-color=true", "status"}, false},
		{"--no-color after --", true, nil, []string{"dev", "--", "npm", "--no-color"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := func(key string) string { return test.env[key] }
			if got := colorEnabled(test.terminal, env, test.args); got != test.want {
				t.Errorf("colorEnabled(%v, %v, %v) = %v, want %v", test.terminal, test.env, test.args, got, test.want)
			}
		})
	}
}
