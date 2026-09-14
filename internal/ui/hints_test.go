package ui

import (
	"bytes"
	"testing"
)

func TestHintIsForPeopleAtATerminalOnly(t *testing.T) {
	for _, test := range []struct {
		name        string
		interactive bool
		format      string
		want        string
	}{
		{"terminal", true, "human", "Next: coolship deploy\n"},
		{"piped or CI", false, "human", ""},
		{"json", true, "json", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			Hint(Streams{Out: &out, Err: &diagnostic, Interactive: test.interactive}, test.format, NextDeploy(""))
			if diagnostic.String() != test.want || out.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", out.String(), diagnostic.String())
			}
		})
	}
}

func TestHintTexts(t *testing.T) {
	if got := NextDeploy("default"); got != "Next: coolship deploy" {
		t.Errorf("NextDeploy(default) = %q", got)
	}
	if got := NextDeploy("api"); got != "Next: coolship deploy --target api" {
		t.Errorf("NextDeploy(api) = %q", got)
	}
	want := "Next: coolship deployments --target api to compare with earlier runs, or coolship open --dashboard --target api to retry from Coolify"
	if got := FailedDeployHint("api"); got != want {
		t.Errorf("FailedDeployHint = %q", got)
	}
}
