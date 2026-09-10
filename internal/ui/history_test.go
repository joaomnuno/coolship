package ui

import (
	"testing"
	"time"
)

func TestDurationFollowsTheServerTimestamps(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 27, 0, 0, time.UTC)
	for _, test := range []struct{ name, status, created, finished, want string }{
		{"finished", "finished", "2026-09-10T12:26:33.000000Z", "2026-09-10T12:26:57.000000Z", "24s"},
		{"still running", "in_progress", "2026-09-10T12:26:17.000000Z", "", "43s"},
		{"queued", "queued", "2026-09-10T12:26:50.000000Z", "", "10s"},
		{"cancelled before the job ran", "cancelled-by-user", "2026-09-10T12:26:17.000000Z", "", ""},
		{"failed without an end", "failed", "2026-09-10T12:26:17.000000Z", "", ""},
		{"unreadable start", "finished", "yesterday", "2026-09-10T12:26:57.000000Z", ""},
		{"unreadable end", "finished", "2026-09-10T12:26:33.000000Z", "later", ""},
		{"end before start", "finished", "2026-09-10T12:26:57.000000Z", "2026-09-10T12:26:33.000000Z", ""},
	} {
		if got := duration(test.status, test.created, test.finished, now); got != test.want {
			t.Errorf("%s: duration = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestShortIdentifiersKeepPlaceholders(t *testing.T) {
	if got := shortID("klmsv7qglx5qdn5gyi5krf7v"); got != "klmsv7qg" {
		t.Errorf("shortID = %q", got)
	}
	if got := shortID("d-1"); got != "d-1" {
		t.Errorf("short shortID = %q", got)
	}
	if got := shortCommit("0cd7c4a692347804dbd076a4d7e11c847e085473"); got != "0cd7c4a" {
		t.Errorf("shortCommit = %q", got)
	}
	for _, kept := range []string{"HEAD", "abc", "", "release-1"} {
		if got := shortCommit(kept); got != kept {
			t.Errorf("shortCommit(%q) = %q", kept, got)
		}
	}
	if got := localTime("not a time"); got != "not a time" {
		t.Errorf("localTime keeps an unreadable value: %q", got)
	}
}
