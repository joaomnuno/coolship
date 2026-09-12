package ui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/joaomnuno/coolship/internal/service"
)

func TestChoiceLabelsShowNamesAndBreakTies(t *testing.T) {
	got := choiceLabels([]service.Choice{
		{ID: "p-1", Name: "Personal", Detail: "p-1"},
		{ID: "p-2", Name: "Work", Detail: "p-2"},
		{ID: "p-3", Name: "Work", Detail: "p-3"},
		{ID: "g-1", Name: "bot", Detail: "acme"},
		{ID: "g-2", Name: "bot", Detail: "acme"},
		{ID: "k-1", Name: "key"},
		{ID: "k-2", Name: "key"},
		{ID: "only-id"},
	})
	want := []string{"Personal", "Work (p-2)", "Work (p-3)", "bot (acme) (g-1)", "bot (acme) (g-2)", "key (k-1)", "key (k-2)", "only-id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %q; want %q", got, want)
	}
}

// drive feeds messages to a picker the way a Bubble Tea program would,
// running the commands each update returns. A command that does not answer
// promptly is a timer (a cursor blink) and is dropped.
func drive(t *testing.T, form *huh.Form, msgs ...tea.Msg) {
	t.Helper()
	var send func(tea.Msg, int)
	var run func(tea.Cmd, int)
	cmdType := reflect.TypeOf(tea.Cmd(nil))
	run = func(cmd tea.Cmd, depth int) {
		if cmd == nil || depth > 50 {
			return
		}
		result := make(chan tea.Msg, 1)
		go func() { result <- cmd() }()
		var msg tea.Msg
		select {
		case msg = <-result:
		case <-time.After(20 * time.Millisecond):
			return
		}
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, next := range batch {
				run(next, depth+1)
			}
			return
		}
		// tea.Sequence's message is an unexported []tea.Cmd.
		if value := reflect.ValueOf(msg); value.Kind() == reflect.Slice && value.Type().Elem() == cmdType {
			for i := range value.Len() {
				run(value.Index(i).Interface().(tea.Cmd), depth+1)
			}
			return
		}
		send(msg, depth+1)
	}
	send = func(msg tea.Msg, depth int) {
		_, cmd := form.Update(msg)
		run(cmd, depth)
	}
	for _, msg := range msgs {
		send(msg, 0)
	}
}

func startPicker(t *testing.T, kind string, choices []service.Choice, height int) (*huh.Form, *string) {
	t.Helper()
	form, value := newPicker(kind, choices, height, newPalette(false))
	drive(t, form, nil)
	_, cmd := form.Update(tea.WindowSizeMsg{Width: 80, Height: height})
	_ = cmd
	return form, value
}

var (
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
)

func typed(text string) []tea.Msg {
	var msgs []tea.Msg
	for _, r := range text {
		msgs = append(msgs, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return msgs
}

var applications = []service.Choice{
	{ID: "a-1", Name: "web", Detail: "a-1"},
	{ID: "a-2", Name: "api", Detail: "a-2", Current: true},
	{ID: "a-3", Name: "worker", Detail: "a-3"},
}

func TestPickerStartsOnTheCurrentChoiceAndMovesWithArrows(t *testing.T) {
	form, value := startPicker(t, "application", applications, 24)
	view := form.View()
	for _, want := range []string{"Select application", "web", "api", "worker"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "a-1") {
		t.Errorf("normal mode shows a UUID:\n%s", view)
	}
	if *value != "a-2" {
		t.Fatalf("cursor starts on %q; want the current a-2", *value)
	}
	drive(t, form, keyDown, keyEnter)
	if form.State != huh.StateCompleted || *value != "a-3" {
		t.Fatalf("state=%v value=%q; want completed on a-3", form.State, *value)
	}
}

func TestPickerEscCancels(t *testing.T) {
	form, _ := startPicker(t, "project", applications, 24)
	drive(t, form, keyDown, keyEsc)
	if form.State != huh.StateAborted {
		t.Fatalf("state=%v; want aborted", form.State)
	}
}

func TestPickerFiltersBySlashOrTypingWhenTheListOutgrowsTheScreen(t *testing.T) {
	form, value := startPicker(t, "application", applications, 24)
	drive(t, form, append(append([]tea.Msg{tea.KeyPressMsg{Code: '/', Text: "/"}}, typed("wor")...), keyEnter)...)
	if form.State != huh.StateCompleted || *value != "a-3" {
		t.Fatalf("slash filter: state=%v value=%q", form.State, *value)
	}
	// Three options and the chrome do not fit in four rows: typing filters.
	form, value = startPicker(t, "application", applications, 4)
	drive(t, form, append(typed("we"), keyEnter)...)
	if form.State != huh.StateCompleted || *value != "a-1" {
		t.Fatalf("typed filter: state=%v value=%q", form.State, *value)
	}
	// Esc still cancels while filtering.
	form, _ = startPicker(t, "application", applications, 4)
	drive(t, form, append(typed("w"), keyEsc)...)
	if form.State != huh.StateAborted {
		t.Fatalf("esc while filtering: state=%v", form.State)
	}
}

func TestSelectListsNamesWhenInputIsNotATerminal(t *testing.T) {
	var diagnostic strings.Builder
	prompter := NewPrompter(Streams{In: strings.NewReader("x\n3\n"), Err: &diagnostic, Interactive: true})
	id, err := prompter.Select(context.Background(), "application", applications)
	if err != nil || id != "a-3" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	out := diagnostic.String()
	for _, want := range []string{"1. web\n", "2. api (current)\n", "3. worker\n", "Enter a listed number"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "a-1") {
		t.Errorf("names only, yet a UUID is shown:\n%s", out)
	}
	prompter = NewPrompter(Streams{In: strings.NewReader("q\n"), Err: &diagnostic, Interactive: true})
	if _, err := prompter.Select(context.Background(), "application", applications); !errors.Is(err, service.ErrCancelled) {
		t.Fatalf("q: %v", err)
	}
}
