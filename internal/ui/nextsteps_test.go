package ui

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
)

func TestNextStepsFollowTheApplicationState(t *testing.T) {
	commands := func(steps []NextStep) []string {
		var result []string
		for _, step := range steps {
			result = append(result, step.Command)
		}
		return result
	}
	for _, test := range []struct {
		name    string
		state   service.ProjectState
		options NextStepOptions
		want    []string
	}{
		{"new application with a local .env", service.ProjectState{EnvFile: ".env", LocalVariables: 3},
			NextStepOptions{}, []string{"coolship env push", "coolship domain set URL", "coolship deploy"}},
		{"variables already on the server", service.ProjectState{EnvFile: ".env", LocalVariables: 3, RemoteVariables: 2, Domains: []string{"https://app.example.com"}},
			NextStepOptions{}, []string{"coolship deploy"}},
		{"generated domain", service.ProjectState{Domains: []string{"https://app-1.10.0.0.1.sslip.io"}, Generated: true},
			NextStepOptions{NoDeploy: true}, []string{"coolship domain set URL"}},
		{"compose has no domain set", service.ProjectState{},
			NextStepOptions{Compose: true, NoDeploy: true}, []string{"coolship domain set SERVICE=URL"}},
		{"compose read from the application", service.ProjectState{Compose: true, Deployed: true},
			NextStepOptions{}, []string{"coolship domain set SERVICE=URL", "coolship logs", "coolship open"}},
		{"compose with its services set", service.ProjectState{Compose: true, Domains: []string{"https://bot.example.com"}},
			NextStepOptions{Compose: true, NoDeploy: true}, nil},
		{"deployed, at most three", service.ProjectState{EnvFile: ".env", LocalVariables: 1, Deployed: true},
			NextStepOptions{Target: "web"}, []string{"coolship env push --target web", "coolship domain set URL --target web", "coolship logs --target web"}},
		{"deployed with everything set", service.ProjectState{Domains: []string{"https://app.example.com"}, Deployed: true},
			NextStepOptions{NoDeploy: true}, []string{"coolship logs", "coolship open"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := commands(NextSteps(test.state, test.options)); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("NextSteps = %q, want %q", got, test.want)
			}
		})
	}
	steps := NextSteps(service.ProjectState{EnvFile: ".env", LocalVariables: 1, Domains: []string{"https://a.example.com"}, Deployed: true}, NextStepOptions{})
	if steps[0].Purpose != "send the 1 variable in .env to Coolify" {
		t.Fatalf("purpose = %q", steps[0].Purpose)
	}
}

func TestHintsPrintOneAlignedBlockForAPerson(t *testing.T) {
	steps := []NextStep{{"coolship env push", "send the 2 variables in .env to Coolify"}, {"coolship deploy", "build and start it"}}
	var stderr bytes.Buffer
	Hints(Streams{Err: &stderr, Interactive: true}, "human", steps)
	want := "Next:\n  coolship env push  send the 2 variables in .env to Coolify\n  coolship deploy    build and start it\n"
	if stderr.String() != want {
		t.Fatalf("hints = %q, want %q", stderr.String(), want)
	}
	for _, streams := range []struct {
		interactive bool
		format      string
	}{{false, "human"}, {true, "json"}} {
		stderr.Reset()
		Hints(Streams{Err: &stderr, Interactive: streams.interactive}, streams.format, steps)
		if stderr.Len() != 0 {
			t.Fatalf("%+v printed %q", streams, stderr.String())
		}
	}
}

func TestYesNoTakesTheDefaultOnEnter(t *testing.T) {
	for _, test := range []struct {
		input      string
		defaultYes bool
		want       bool
		prompt     string
	}{
		{"\n", true, true, "Deploy? [Y/n] "},
		{"\n", false, false, "Deploy? [y/N] "},
		{"yes\n", false, true, "Deploy? [y/N] "},
		{"N\n", true, false, "Deploy? [Y/n] "},
		{"maybe\ny\n", false, true, "Deploy? [y/N] Answer y or n.\nDeploy? [y/N] "},
	} {
		var stderr bytes.Buffer
		prompter := NewPrompter(Streams{In: strings.NewReader(test.input), Err: &stderr, Interactive: true})
		got, err := prompter.YesNo(context.Background(), "Deploy?", test.defaultYes)
		if err != nil || got != test.want || stderr.String() != test.prompt {
			t.Errorf("%q default %v: got %v err %v prompt %q", test.input, test.defaultYes, got, err, stderr.String())
		}
	}
	// Nobody to ask answers no, silently.
	var stderr bytes.Buffer
	if got, err := NewPrompter(Streams{Err: &stderr}).YesNo(context.Background(), "Deploy?", true); got || err != nil || stderr.Len() != 0 {
		t.Fatalf("noninteractive: %v %v %q", got, err, stderr.String())
	}
}

func TestReportedErrorsAreNotPrintedTwice(t *testing.T) {
	var stderr bytes.Buffer
	err := Reported(&service.InputError{Err: context.DeadlineExceeded})
	if writeErr := ReportError(Streams{Err: &stderr}, "human", err); writeErr != nil || stderr.Len() != 0 {
		t.Fatalf("reported error printed %q (%v)", stderr.String(), writeErr)
	}
	if ExitCode(err) != 2 {
		t.Fatalf("exit code = %d, want 2", ExitCode(err))
	}
}
