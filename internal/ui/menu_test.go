package ui

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/joaomnuno/coolship/internal/service"
)

var errTestNotLinked = errors.New("project is not linked; run coolship link")

var menuNow = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func testMenuGroups() []MenuGroup {
	return []MenuGroup{
		{Title: "Get started", Verbs: []MenuVerb{
			{Path: []string{"login"}, Short: "Save a Coolify instance"},
			{Path: []string{"init"}, Short: "Create an application"},
			{Path: []string{"link"}, Short: "Bind this repository"},
		}},
		{Title: "Ship", Verbs: []MenuVerb{
			{Path: []string{"deploy"}, Short: "Deploy the linked application"},
			{Path: []string{"preview"}, Short: "Deploy a preview", Inputs: []MenuInput{{Title: "Pull request number", Flag: "pr"}}},
			{Path: []string{"deployments"}, Short: "List deployments"},
		}},
		{Title: "Configure", Verbs: []MenuVerb{
			{Path: []string{"env", "pull"}, Short: "Pull variables"},
			{Path: []string{"env", "push"}, Short: "Push variables"},
			{Path: []string{"domain", "set"}, Short: "Replace domains", Inputs: []MenuInput{{Title: "Domains", Fields: true}}},
		}},
	}
}

func testStatus() service.StatusResult {
	return service.StatusResult{
		Target: service.TargetInfo{Target: "default", Instance: "home", Project: "coolship-example",
			Environment: "production", Application: "coolship-example"},
		Status: "running:healthy",
		URL:    "https://coolship.example.com",
		LastDeployment: &service.DeploymentSummary{UUID: "dep-1", Status: "finished",
			Commit: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", CreatedAt: "2026-09-14T09:00:00Z"},
	}
}

// newTestMenu builds a colourless menu whose header read is load, or one
// that returns testStatus when load is nil.
func newTestMenu(t *testing.T, refresh time.Duration, load func(context.Context) (service.StatusResult, error)) menuModel {
	t.Helper()
	if load == nil {
		load = func(context.Context) (service.StatusResult, error) { return testStatus(), nil }
	}
	model := newMenuModel(context.Background(), newPalette(false), MenuOptions{
		Groups:    testMenuGroups(),
		Status:    load,
		NotLinked: func(err error) bool { return errors.Is(err, errTestNotLinked) },
		Refresh:   refresh,
		Now:       func() time.Time { return menuNow },
	})
	t.Cleanup(model.stopReading)
	return model
}

func press(text string) tea.KeyPressMsg {
	switch text {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(text)[0], Text: text}
}

// step sends messages in order and returns the model and the last command.
func step(t *testing.T, model menuModel, msgs ...tea.Msg) (menuModel, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, msg := range msgs {
		var next tea.Model
		next, cmd = model.Update(msg)
		model = next.(menuModel)
	}
	return model, cmd
}

// loaded delivers the first read's answer.
func loaded(t *testing.T, model menuModel, result service.StatusResult, err error) (menuModel, tea.Cmd) {
	t.Helper()
	return step(t, model, menuStatusMsg{gen: model.gen, result: result, err: err})
}

func viewText(model menuModel) string { return model.View().Content }

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestMenuShowsLoadingThenHeader(t *testing.T) {
	model := newTestMenu(t, 0, nil)
	view := viewText(model)
	if !strings.Contains(view, "Reading application status") || !strings.Contains(view, "deploy") {
		t.Fatalf("loading view does not show the spinner and the verbs: %q", view)
	}
	model, _ = loaded(t, model, testStatus(), nil)
	view = viewText(model)
	for _, want := range []string{
		"coolship-example  production · project coolship-example · context home",
		"● running:healthy  https://coolship.example.com",
		"Last deployment: ✓ finished  a1b2c3d  3 h ago",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Reading application status") || strings.Contains(view, "\x1b[") {
		t.Errorf("view keeps the spinner or styles a colourless stream:\n%s", view)
	}
	if !model.View().AltScreen {
		t.Error("menu does not use the alternate screen")
	}
}

func TestMenuListsGroupsInOrderWithInputHints(t *testing.T) {
	view := viewText(newTestMenu(t, 0, nil))
	position := 0
	for _, want := range []string{"Get started", "› login", "init", "link", "Ship", "deploy", "preview…", "deployments", "Configure", "env pull", "env push", "domain set…"} {
		index := strings.Index(view[position:], want)
		if index < 0 {
			t.Fatalf("view lacks %q after offset %d:\n%s", want, position, view)
		}
		position += index + len(want)
	}
}

func TestMenuNotLinkedStillOffersLinkAndInit(t *testing.T) {
	model := newTestMenu(t, time.Second, nil)
	model, cmd := loaded(t, model, service.StatusResult{}, &service.InputError{Err: errTestNotLinked})
	if cmd != nil {
		t.Error("a directory that is not linked is read again on a timer")
	}
	view := viewText(model)
	for _, want := range []string{"○ not linked", "link", "init"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	model, cmd = step(t, model, press("down"), press("enter"))
	if !isQuit(cmd) || model.chosen == nil || model.chosen.Name() != "init" {
		t.Fatalf("enter chose %+v, quit %v", model.chosen, isQuit(cmd))
	}
}

func TestMenuShowsAFailedRead(t *testing.T) {
	model, _ := loaded(t, newTestMenu(t, 0, nil), service.StatusResult{}, errors.New("server unreachable"))
	view := viewText(model)
	if !strings.Contains(view, "✗ Status unavailable") || !strings.Contains(view, "server unreachable") {
		t.Fatalf("view = %q", view)
	}
}

func TestMenuTellsAnUnreadableHistoryFromAnEmptyOne(t *testing.T) {
	empty := testStatus()
	empty.LastDeployment = nil
	empty.Warnings = []string{"COOLSHIP_URL and COOLSHIP_TOKEN select this invocation's instance; committed context is not used."}
	model, _ := loaded(t, newTestMenu(t, 0, nil), empty, nil)
	if view := viewText(model); !strings.Contains(view, "Last deployment: none yet") {
		t.Fatalf("empty history with an unrelated warning:\n%s", view)
	}
	unreadable := empty
	unreadable.Warnings = append(slices.Clone(empty.Warnings), service.HistoryUnreadableWarning+"403 Forbidden")
	model, _ = loaded(t, newTestMenu(t, 0, nil), unreadable, nil)
	view := viewText(model)
	if !strings.Contains(view, "Last deployment: unavailable  403 Forbidden") || strings.Contains(view, "none yet") {
		t.Fatalf("unreadable history:\n%s", view)
	}
}

func TestMenuFitsATerminalTooShortForTheHeader(t *testing.T) {
	for height := 1; height <= headerLines+footerLines+1; height++ {
		model := newTestMenu(t, 0, nil)
		model, _ = step(t, model, tea.WindowSizeMsg{Width: 60, Height: height})
		model, _ = loaded(t, model, testStatus(), nil)
		for _, keys := range [][]string{nil, {"up"}, {"s"}} {
			current := model
			for _, key := range keys {
				current, _ = step(t, current, press(key))
			}
			view := viewText(current)
			if lines := strings.Count(view, "\n") + 1; lines > height {
				t.Errorf("height %d after %q: view is %d lines:\n%s", height, keys, lines, view)
			}
			if !strings.Contains(view, "› ") {
				t.Errorf("height %d after %q: the selection is not in view:\n%s", height, keys, view)
			}
		}
	}
}

func TestMenuMovesAndChooses(t *testing.T) {
	model := newTestMenu(t, 0, nil)
	model, _ = step(t, model, press("down"), press("j"), press("j"), press("k"), press("down"), press("down"))
	if model.cursor != 4 {
		t.Fatalf("cursor = %d; want 4", model.cursor)
	}
	model, cmd := step(t, model, press("enter"))
	if !isQuit(cmd) || model.chosen == nil || model.chosen.Name() != "preview" {
		t.Fatalf("chose %+v", model.chosen)
	}
	// Up from the top wraps to the last verb.
	model, _ = step(t, newTestMenu(t, 0, nil), press("up"))
	if got := model.visible()[model.cursor].verb.Name(); got != "domain set" {
		t.Fatalf("up from the top = %q", got)
	}
}

func TestMenuFiltersByTyping(t *testing.T) {
	model := newTestMenu(t, 0, nil)
	model, _ = step(t, model, press("e"), press("n"), press("v"), press(" "), press("p"))
	if names := visibleNames(model); !reflect.DeepEqual(names, []string{"env pull", "env push"}) {
		t.Fatalf("filter %q shows %q", model.filter, names)
	}
	if view := viewText(model); !strings.Contains(view, "/ env p") || strings.Contains(view, "Get started") {
		t.Fatalf("filtered view:\n%s", view)
	}
	// While filtering, j, k, q, and r are text.
	model, cmd := step(t, model, press("u"), press("r"))
	if cmd != nil || model.filter != "env pur" || len(model.visible()) != 0 {
		t.Fatalf("filter = %q, cmd %v", model.filter, cmd != nil)
	}
	if view := viewText(model); !strings.Contains(view, `No command matches "env pur"`) {
		t.Fatalf("empty filter view:\n%s", view)
	}
	model, _ = step(t, model, press("backspace"), press("s"), press("enter"))
	if model.chosen == nil || model.chosen.Name() != "env push" {
		t.Fatalf("chose %+v", model.chosen)
	}
	// Esc clears the filter first and quits only after.
	model, _ = step(t, newTestMenu(t, 0, nil), press("/"), press("r"))
	if model.filter != "r" {
		t.Fatalf("slash then r filtered %q", model.filter)
	}
	model, cmd = step(t, model, press("esc"))
	if isQuit(cmd) || model.filtering || len(model.visible()) != 9 {
		t.Fatalf("esc did not clear the filter: %q", model.filter)
	}
	if _, cmd = step(t, model, press("esc")); !isQuit(cmd) {
		t.Fatal("second esc did not quit")
	}
}

func visibleNames(model menuModel) []string {
	var names []string
	for _, entry := range model.visible() {
		names = append(names, entry.verb.Name())
	}
	return names
}

func TestMenuQuitsWithoutChoosing(t *testing.T) {
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		model, cmd := step(t, newTestMenu(t, 0, nil), press(key))
		if !isQuit(cmd) || model.chosen != nil {
			t.Errorf("%s: quit %v, chosen %+v", key, isQuit(cmd), model.chosen)
		}
	}
}

func TestMenuRefreshesOnATimerAndOnR(t *testing.T) {
	status := testStatus()
	calls := 0
	model := newTestMenu(t, time.Second, func(context.Context) (service.StatusResult, error) {
		calls++
		return status, nil
	})
	first := model.gen
	model, cmd := loaded(t, model, status, nil)
	if cmd == nil {
		t.Fatal("no refresh scheduled after a read")
	}
	// The timer reads again, and marks the header as refreshing meanwhile.
	model, cmd = step(t, model, menuRefreshMsg{gen: first})
	if !model.loading || cmd == nil || model.gen != first+1 {
		t.Fatal("refresh timer did not start a read")
	}
	msg := cmd().(menuStatusMsg)
	if calls != 1 || msg.gen != first+1 {
		t.Fatalf("calls = %d, gen = %d", calls, msg.gen)
	}
	status.Status = "exited:unhealthy"
	model, _ = loaded(t, model, status, nil)
	if view := viewText(model); !strings.Contains(view, "● exited:unhealthy") {
		t.Fatalf("refreshed view:\n%s", view)
	}
	// r starts a read now; the old timer and a slower read are dropped.
	model, cmd = step(t, model, press("r"))
	if cmd == nil || model.gen != first+2 {
		t.Fatalf("r did not read again (gen %d)", model.gen)
	}
	if _, cmd = step(t, model, menuRefreshMsg{gen: first + 1}); cmd != nil {
		t.Fatal("a superseded timer started a read")
	}
	stale := status
	stale.Status = "running:healthy"
	model, _ = step(t, model, menuStatusMsg{gen: first + 1, result: stale})
	if strings.Contains(viewText(model), "running:healthy") {
		t.Fatal("a superseded read replaced the header")
	}
}

// blockingReads is a header reader that waits for its context, recording
// each read's context so a test can see which were cancelled.
type blockingReads struct {
	started chan context.Context
}

func (b blockingReads) load(ctx context.Context) (service.StatusResult, error) {
	b.started <- ctx
	<-ctx.Done()
	return service.StatusResult{}, ctx.Err()
}

// startedRead runs a read command in the background and returns the context
// it was given.
func startedRead(t *testing.T, reads blockingReads, cmd tea.Cmd) context.Context {
	t.Helper()
	if cmd == nil {
		t.Fatal("no read command")
	}
	go cmd()
	select {
	case ctx := <-reads.started:
		return ctx
	case <-time.After(time.Second):
		t.Fatal("read did not start")
	}
	return nil
}

func cancelled(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(time.Second):
		return false
	}
}

func TestMenuCancelsASupersededRead(t *testing.T) {
	reads := blockingReads{started: make(chan context.Context, 4)}
	model := newTestMenu(t, 0, reads.load)
	first := startedRead(t, reads, model.read())
	model, cmd := step(t, model, press("r"))
	if !cancelled(first) {
		t.Fatal("r left the earlier read running")
	}
	second := startedRead(t, reads, cmd)
	if second.Err() != nil {
		t.Fatal("the new read started cancelled")
	}
	// Choosing a verb ends the read in flight too.
	if _, cmd = step(t, model, press("enter")); !isQuit(cmd) || !cancelled(second) {
		t.Fatal("choosing a verb left the read running")
	}
}

func TestMenuQuitCancelsTheReadInFlight(t *testing.T) {
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		reads := blockingReads{started: make(chan context.Context, 1)}
		model := newTestMenu(t, 0, reads.load)
		ctx := startedRead(t, reads, model.read())
		if _, cmd := step(t, model, press(key)); !isQuit(cmd) || !cancelled(ctx) {
			t.Errorf("%s left the read running", key)
		}
	}
}

func TestMenuFitsANarrowTerminal(t *testing.T) {
	status := testStatus()
	status.Target.Application = strings.Repeat("very-long-application-name-", 4)
	status.URL = "https://" + strings.Repeat("sub.", 20) + "example.com"
	model := newTestMenu(t, 0, nil)
	model, _ = step(t, model, tea.WindowSizeMsg{Width: 30, Height: 16})
	model, _ = loaded(t, model, status, nil)
	view := viewText(model)
	lines := strings.Split(view, "\n")
	if len(lines) > 16 {
		t.Fatalf("view is %d lines for a 16-line terminal:\n%s", len(lines), view)
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width > 30 {
			t.Errorf("line is %d cells wide on a 30-cell terminal: %q", width, line)
		}
	}
	if !strings.Contains(lines[1], "very-long-application-name") || !strings.HasSuffix(lines[1], "…") {
		t.Fatalf("long header line is not cut with an ellipsis: %q", lines[1])
	}
	// Colour on, the cut still counts cells, not escape bytes.
	colour := newMenuModel(context.Background(), newPalette(true), MenuOptions{Groups: testMenuGroups(), Now: func() time.Time { return menuNow }})
	colour, _ = step(t, colour, tea.WindowSizeMsg{Width: 30, Height: 16}, menuStatusMsg{gen: colour.gen, result: status})
	for _, line := range strings.Split(viewText(colour), "\n") {
		if width := ansi.StringWidth(line); width > 30 {
			t.Errorf("styled line is %d cells wide: %q", width, line)
		}
	}
}

func TestMenuScrollsToKeepTheCursorInView(t *testing.T) {
	model := newTestMenu(t, 0, nil)
	model, _ = step(t, model, tea.WindowSizeMsg{Width: 60, Height: 14})
	model, _ = step(t, model, press("up")) // wraps to domain set, the last verb
	view := viewText(model)
	if !strings.Contains(view, "› domain set…") || strings.Contains(view, "login") {
		t.Fatalf("scrolled view:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > 14 {
		t.Fatalf("view is %d lines for a 14-line terminal:\n%s", lines, view)
	}
	model, _ = step(t, model, press("down")) // wraps to login, under its title
	if view = viewText(model); !strings.Contains(view, "Get started\n› login") {
		t.Fatalf("view after wrapping down:\n%s", view)
	}
}

func TestVerbArgsLayAnswersOutAsTyped(t *testing.T) {
	verb := MenuVerb{Path: []string{"domain", "set"}, Inputs: []MenuInput{
		{Title: "Domains", Fields: true},
		{Title: "Redirect", Flag: "redirect"},
	}}
	got := verbArgs(verb, []string{" a.example.com  b.example.com ", "www"})
	want := []string{"domain", "set", "--redirect=www", "--", "a.example.com", "b.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q; want %q", got, want)
	}
	logout := MenuVerb{Path: []string{"logout"}, Inputs: []MenuInput{{Title: "Context"}}}
	if got := verbArgs(logout, []string{"-x"}); !reflect.DeepEqual(got, []string{"logout", "--", "-x"}) {
		t.Fatalf("dashed positional args = %q", got)
	}
	preview := MenuVerb{Path: []string{"preview"}, Inputs: []MenuInput{{Title: "PR", Flag: "pr"}}}
	if got := verbArgs(preview, []string{"42"}); !reflect.DeepEqual(got, []string{"preview", "--pr=42"}) {
		t.Fatalf("flag-only args = %q", got)
	}
	if got := verbArgs(MenuVerb{Path: []string{"status"}}, nil); !reflect.DeepEqual(got, []string{"status"}) {
		t.Fatalf("no-input args = %q", got)
	}
}

func TestInputPromptEscReopensAndCtrlCLeaves(t *testing.T) {
	var interrupted atomic.Bool
	filter := interruptFilter(&interrupted)
	filter(nil, press("esc"))
	reopen, err := inputOutcome(huh.ErrUserAborted, interrupted.Load())
	if !reopen || err != nil {
		t.Fatalf("esc: reopen %v, err %v", reopen, err)
	}
	if msg := filter(nil, press("ctrl+c")); msg != tea.Msg(press("ctrl+c")) {
		t.Fatal("the filter changed the key")
	}
	reopen, err = inputOutcome(huh.ErrUserAborted, interrupted.Load())
	if reopen || err != nil {
		t.Fatalf("ctrl+c: reopen %v, err %v", reopen, err)
	}
	failure := errors.New("terminal gone")
	if reopen, err = inputOutcome(failure, false); reopen || !errors.Is(err, failure) {
		t.Fatalf("failure: reopen %v, err %v", reopen, err)
	}
	if reopen, err = inputOutcome(nil, false); reopen || err != nil {
		t.Fatalf("answered: reopen %v, err %v", reopen, err)
	}
}

func TestInputFormHasAFieldPerInput(t *testing.T) {
	form, answers := newInputForm(MenuVerb{Path: []string{"preview"}, Inputs: []MenuInput{{Title: "Pull request number", Flag: "pr", Validate: RequirePositiveNumber}}}, newPalette(false))
	if form == nil || len(answers) != 1 {
		t.Fatalf("form %v answers %d", form, len(answers))
	}
}

func TestMenuValidators(t *testing.T) {
	if RequireText("  ") == nil || RequireText("home") != nil {
		t.Error("RequireText")
	}
	for value, ok := range map[string]bool{"42": true, " 7 ": true, "0": false, "-1": false, "x": false, "": false} {
		if (RequirePositiveNumber(value) == nil) != ok {
			t.Errorf("RequirePositiveNumber(%q)", value)
		}
	}
}

func TestRunMenuRefusesWithoutATerminal(t *testing.T) {
	_, err := RunMenu(context.Background(), Streams{In: strings.NewReader(""), Out: &strings.Builder{}}, MenuOptions{})
	if !errors.Is(err, ErrMenuNeedsTerminal) || ExitCode(err) != 2 {
		t.Fatalf("err = %v, exit %d", err, ExitCode(err))
	}
	if !strings.Contains(err.Error(), "coolship help") {
		t.Fatalf("message does not point at help: %v", err)
	}
}

func TestMenuOpensOnlyWhenInteractiveOnBothTerminals(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		inTerminal  bool
		outTerminal bool
		open        bool
	}{
		{name: "interactive on both terminals", interactive: true, inTerminal: true, outTerminal: true, open: true},
		{name: "CI with pseudo-terminals", inTerminal: true, outTerminal: true},
		{name: "piped stdin", interactive: true, outTerminal: true},
		{name: "piped stdout", interactive: true, inTerminal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if open := menuOpens(test.interactive, test.inTerminal, test.outTerminal); open != test.open {
				t.Fatalf("menuOpens(%v, %v, %v) = %v", test.interactive, test.inTerminal, test.outTerminal, open)
			}
		})
	}
}
