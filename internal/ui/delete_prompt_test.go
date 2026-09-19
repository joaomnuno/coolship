package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
)

func TestConfirmDeleteShowsWhatGoesAndDefaultsToNo(t *testing.T) {
	plan := service.DeletePlan{
		Target: service.TargetInfo{Application: "api", ApplicationUUID: "app-1", Environment: "production", Project: "Personal"},
		Status: "running:healthy",
		URLs:   []string{"https://api.example.com", "https://api.internal.example.com"},
	}
	var diagnostic bytes.Buffer
	prompter := NewPrompter(Streams{In: strings.NewReader("y\n"), Err: &diagnostic, Interactive: true})
	ok, err := prompter.ConfirmDelete(context.Background(), plan)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	text := diagnostic.String()
	for _, want := range []string{
		"Delete api in production?",
		"Application: api (app-1)",
		"Environment: production",
		"Status:      running:healthy",
		"URL:         https://api.example.com",
		"             https://api.internal.example.com",
		"Volumes:     deleted with the application",
		"This cannot be undone.",
		"Confirm [y/N]: ",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, text)
		}
	}
	// Nothing about a binding is shown when the binding is being kept.
	if strings.Contains(text, "Binding:") {
		t.Fatalf("prompt names a binding it does not touch:\n%s", text)
	}

	// Anything but yes declines, so a stray keystroke cannot delete.
	for _, answer := range []string{"\n", "n\n", "yes please\n", "Y es\n"} {
		diagnostic.Reset()
		prompter = NewPrompter(Streams{In: strings.NewReader(answer), Err: &diagnostic, Interactive: true})
		ok, err := prompter.ConfirmDelete(context.Background(), plan)
		if err != nil || ok {
			t.Fatalf("answer %q: ok=%v err=%v", answer, ok, err)
		}
	}
	for _, answer := range []string{"y\n", "Y\n", "yes\n", "YES\n"} {
		diagnostic.Reset()
		prompter = NewPrompter(Streams{In: strings.NewReader(answer), Err: &diagnostic, Interactive: true})
		ok, err := prompter.ConfirmDelete(context.Background(), plan)
		if err != nil || !ok {
			t.Fatalf("answer %q: ok=%v err=%v", answer, ok, err)
		}
	}
}

func TestConfirmDeleteNamesKeptVolumesAndTheBindingItAlsoDeletes(t *testing.T) {
	plan := service.DeletePlan{
		Target:      service.TargetInfo{Application: "api", ApplicationUUID: "app-1", Environment: "staging", Project: "Personal"},
		Status:      "exited:unhealthy",
		KeepVolumes: true,
		Unlink:      true,
		ConfigPath:  "/p/coolship.toml",
	}
	var diagnostic bytes.Buffer
	prompter := NewPrompter(Streams{In: strings.NewReader("y\n"), Err: &diagnostic, Interactive: true})
	if _, err := prompter.ConfirmDelete(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	text := diagnostic.String()
	if !strings.Contains(text, "Volumes:     kept on the server") {
		t.Fatalf("kept volumes are not stated:\n%s", text)
	}
	if !strings.Contains(text, "Binding:     /p/coolship.toml is deleted too") {
		t.Fatalf("the binding it also deletes is not stated:\n%s", text)
	}
}

func TestConfirmDeleteRefusesWithoutAWayToAsk(t *testing.T) {
	var diagnostic bytes.Buffer
	prompter := NewPrompter(Streams{Err: &diagnostic})
	_, err := prompter.ConfirmDelete(context.Background(), service.DeletePlan{})
	var inputErr *service.InputError
	if err == nil || !errors.As(err, &inputErr) || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("err %v", err)
	}
	if diagnostic.String() != "" {
		t.Fatalf("a refusal must not draw the plan: %q", diagnostic.String())
	}
}
