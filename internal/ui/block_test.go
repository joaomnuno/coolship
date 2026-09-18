package ui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
)

// testBlock is the bar as a terminal of the given width draws it.
func testBlock(color bool, width int) *Block {
	return newBlock(newPalette(color), func() int { return width })
}

const probeWarning = "https://github.com/o/r is not reachable anonymously (git ls-remote: exit status 128)."

func initResult() service.InitResult {
	plan := service.InitPlan{Path: "/p/coolship.toml", Directory: "/p", Name: "site", Instance: "home", Repository: "https://github.com/o/r", Branch: "main",
		BuildPack: "dockercompose", ComposeFile: "/compose.yml", Project: "Personal", Environment: "production", Server: "Master",
		Source: service.SourceGitHubApp, GitHubApp: "docs", Private: probeWarning,
		Warnings: []string{probeWarning, "No service has a domain yet."}}
	return service.InitResult{Plan: plan, URL: "https://site.example.com", Warnings: append([]string{}, plan.Warnings...),
		Target: service.TargetInfo{Application: "site", ApplicationUUID: "a-9", Environment: "production", Project: "Personal", Instance: "home"}}
}

// TestBlockBarIsHuhsFocusedBorder pins the bar to the glyph and padding Huh
// draws beside a focused field, with or without colour: a plain character.
func TestBlockBarIsHuhsFocusedBorder(t *testing.T) {
	for _, color := range []bool{false, true} {
		if bar := testBlock(color, 80).bar; bar != "┃ " {
			t.Fatalf("color=%v bar %q", color, bar)
		}
	}
	var none *Block
	if none.Active() || none.prompt("x") != "x" || strings.Join(none.lines("a\nb"), "|") != "a|b" {
		t.Fatal("a nil block changed text")
	}
	if NewBlock(Streams{Err: &bytes.Buffer{}, Interactive: true}, "human") != nil {
		t.Fatal("a buffer is not a terminal, so it has no bar")
	}
}

// TestBlockWrapsInsideTheBar checks a narrow terminal: a long line continues
// on the next row behind the bar, styled text included, and no row is wider
// than the terminal.
func TestBlockWrapsInsideTheBar(t *testing.T) {
	block := testBlock(true, 24)
	lines := block.lines(newPalette(true).apply(dim, "coolship domain set URL  give the application a domain"))
	if len(lines) < 3 {
		t.Fatalf("not wrapped: %q", lines)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "┃ ") || len([]rune(stripANSI(line))) > 24 {
			t.Fatalf("row %q breaks the bar or the width", line)
		}
	}
	// A value continues under itself, so a key column holds.
	hanging := testBlock(false, 30).hanging("  Repository   ", "github.com/o/a-long-repository (main), private")
	if len(hanging) < 2 || !strings.HasPrefix(hanging[0], "┃   Repository   github.com/") || !strings.HasPrefix(hanging[1], "┃                ") {
		t.Fatalf("hanging %q", hanging)
	}
	for _, line := range hanging {
		if len([]rune(line)) > 30 {
			t.Fatalf("row %q is wider than the terminal", line)
		}
	}
}

func stripANSI(text string) string {
	var s screen
	if err := s.replay(text); err != nil {
		return text
	}
	return s.text()
}

// TestInitResultIsUnchangedOffATerminal pins the full result a pipe, a
// verbose run, and a run without a block always had, byte for byte, and
// that a confirmation which showed the warnings is not followed by them
// again.
func TestInitResultIsUnchangedOffATerminal(t *testing.T) {
	const full = "Created application site (a-9) from https://github.com/o/r at main\nBuild pack: dockercompose\nCompose file: /compose.yml\nSource: GitHub App docs\nURL: https://site.example.com\n" +
		"Linked project in /p/coolship.toml\nApplication: site (a-9)\nEnvironment: production\nProject: Personal\nContext: home\n"
	warnings := "Warning: " + probeWarning + "\nWarning: No service has a domain yet.\n"
	for _, test := range []struct {
		name      string
		streams   Streams
		confirmed bool
		stderr    string
	}{
		{"piped", Streams{}, false, warnings},
		{"piped after a confirmation", Streams{Interactive: true}, true, ""},
		{"piped stdout beside a block", Streams{Interactive: true, Block: testBlock(false, 80)}, true, ""},
	} {
		var out, diagnostic bytes.Buffer
		test.streams.Out, test.streams.Err = &out, &diagnostic
		result := initResult()
		result.Warnings = append(result.Warnings, "Written later.")
		if err := NewRenderer(test.streams, "human").Init(result, test.confirmed); err != nil {
			t.Fatal(err)
		}
		wantErr := test.stderr + "Warning: Written later.\n"
		if test.streams.Block != nil {
			wantErr = "┃ Warning: Written later.\n"
		}
		if out.String() != full || diagnostic.String() != wantErr {
			t.Fatalf("%s\nstdout %q\nstderr %q", test.name, out.String(), diagnostic.String())
		}
	}
	var out bytes.Buffer
	if err := NewRenderer(Streams{Out: &out}, "human").Link(service.LinkResult{Path: "/p/coolship.toml", Target: initResult().Target}); err != nil {
		t.Fatal(err)
	}
	if want := "Linked project in /p/coolship.toml\nApplication: site (a-9)\nEnvironment: production\nProject: Personal\nContext: home\n"; out.String() != want {
		t.Fatalf("link %q", out.String())
	}
}

// TestInitAndLinkEndWithOneLineInsideABlock checks the terminal ending: one
// line inside the bar that names the application and its URL, nothing the
// plan or the checklist already said, and no warning a second time.
func TestInitAndLinkEndWithOneLineInsideABlock(t *testing.T) {
	render := func(result service.InitResult, confirmed bool) (string, string) {
		var out, diagnostic bytes.Buffer
		streams := Streams{Out: &out, Err: &diagnostic, Interactive: true, OutTerminal: true, ErrTerminal: true, Block: testBlock(false, 80)}
		if err := NewRenderer(streams, "human").Init(result, confirmed); err != nil {
			t.Fatal(err)
		}
		return out.String(), diagnostic.String()
	}
	if out, diagnostic := render(initResult(), true); out != "┃ site is linked: https://site.example.com\n" || diagnostic != "" {
		t.Fatalf("stdout %q stderr %q", out, diagnostic)
	}
	// With --yes nothing was confirmed: the warnings are printed once,
	// inside the bar, without the probe's own words.
	if out, diagnostic := render(initResult(), false); out != "┃ site is linked: https://site.example.com\n" || diagnostic != "┃ Warning: No service has a domain yet.\n" {
		t.Fatalf("stdout %q stderr %q", out, diagnostic)
	}
	noURL := initResult()
	noURL.URL = ""
	if out, _ := render(noURL, true); out != "┃ site is linked.\n" {
		t.Fatalf("stdout %q", out)
	}
	// A deployment's progress ended the block; its result follows as deploy's.
	deployed := initResult()
	deployed.Deployment = &service.DeployResult{DeploymentUUID: "d-123456789", Status: "finished", Target: deployed.Target}
	if out, _ := render(deployed, true); out != "site is linked: https://site.example.com\nDeployment: d-123456\nApplication: site\nStatus: finished\n" {
		t.Fatalf("stdout %q", out)
	}
	// A created deploy key keeps its instructions.
	key := service.InitResult{Plan: service.InitPlan{Instance: "home"}, DeployKey: &service.DeployKeyResult{Name: "ci", UUID: "k-1", PublicKey: "ssh-ed25519 AAAA", Repository: "git@github.com:o/r.git"}}
	if out, _ := render(key, true); !strings.HasPrefix(out, "Created deploy key ci on home\n") || strings.Contains(out, "┃") {
		t.Fatalf("stdout %q", out)
	}

	var out bytes.Buffer
	streams := Streams{Out: &out, Interactive: true, OutTerminal: true, Block: testBlock(false, 80)}
	if err := NewRenderer(streams, "human").Link(service.LinkResult{Path: "/p/coolship.toml", Target: initResult().Target}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "┃ site is linked.\n" {
		t.Fatalf("link %q", out.String())
	}
	// JSON never changes.
	out.Reset()
	if err := NewRenderer(streams, "json").Link(service.LinkResult{Path: "/p/coolship.toml", Target: initResult().Target}); err != nil || !strings.HasPrefix(out.String(), "{") {
		t.Fatalf("json %q err=%v", out.String(), err)
	}
}

// TestConfirmInitPlanInsideABlock checks the terminal plan: an aligned table
// without colons, private on the repository line in place of the probe's
// warning, the other warning once above the question, and every line, the
// open question included, behind the bar.
func TestConfirmInitPlanInsideABlock(t *testing.T) {
	plan := initResult().Plan
	plan.Port = 0
	var diagnostic bytes.Buffer
	streams := Streams{In: strings.NewReader("y\n"), Err: &diagnostic, Interactive: true, Block: testBlock(false, 90)}
	accepted, err := NewPrompter(streams).ConfirmInit(context.Background(), plan)
	if err != nil || !accepted {
		t.Fatalf("accepted=%v err=%v", accepted, err)
	}
	want := strings.Join([]string{
		"┃ Warning: No service has a domain yet.",
		"┃ Create application site on home?",
		"┃   Repository       github.com/o/r (main), private",
		"┃   Source           GitHub App docs",
		"┃   Build pack       dockercompose",
		"┃     Compose file   /compose.yml",
		"┃   Project          Personal / production",
		"┃   Server           Master",
		"┃   Binding          ./coolship.toml",
		"┃ Confirm [y/N]: ",
	}, "\n")
	if diagnostic.String() != want {
		t.Fatalf("plan\n got %q\nwant %q", diagnostic.String(), want)
	}

	// Without a block the plan is the text it was, the probe's words included.
	diagnostic.Reset()
	streams = Streams{In: strings.NewReader("n\n"), Err: &diagnostic, Interactive: true}
	if _, err := NewPrompter(streams).ConfirmInit(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"Warning: " + probeWarning + "\n", "  Repository:  https://github.com/o/r (branch main)\n", "  Environment: production\n", "  Binding:     /p/coolship.toml\n"} {
		if !strings.Contains(diagnostic.String(), line) {
			t.Fatalf("plain plan lacks %q: %q", line, diagnostic.String())
		}
	}
	if strings.Contains(diagnostic.String(), "┃") || strings.Contains(diagnostic.String(), "private") {
		t.Fatalf("plain plan changed: %q", diagnostic.String())
	}
}

// TestLoginAfterFormSaysOnlyWhatTheChecklistDidNot checks the ending of a
// login asked through the form: one line on a terminal, the full result for
// a pipe.
func TestLoginAfterFormSaysOnlyWhatTheChecklistDidNot(t *testing.T) {
	result := service.LoginResult{Name: "home", URL: "https://coolify.example.com", Team: "Personal", Server: "4.3.18", Default: true, Path: "/c/config.json"}
	var out bytes.Buffer
	if err := NewRenderer(Streams{Out: &out, OutTerminal: true}, "human").LoginAfterForm(result); err != nil || out.String() != "Logged in to home, now the default.\n" {
		t.Fatalf("terminal %q err=%v", out.String(), err)
	}
	out.Reset()
	want := "Logged in to home (https://coolify.example.com) as team Personal on Coolify 4.3.18, now the default\nSaved to /c/config.json\n"
	if err := NewRenderer(Streams{Out: &out}, "human").LoginAfterForm(result); err != nil || out.String() != want {
		t.Fatalf("piped %q err=%v", out.String(), err)
	}
}

func TestPlanLabels(t *testing.T) {
	for remote, want := range map[string]string{
		"https://github.com/joaomnuno/ResolveIST":     "github.com/joaomnuno/ResolveIST",
		"git@github.com:joaomnuno/ResolveIST.git":     "github.com/joaomnuno/ResolveIST",
		"ssh://git@gitea.example.com:2222/owner/app/": "gitea.example.com:2222/owner/app",
		"odd": "odd",
	} {
		if got := repositoryLabel(remote); got != want {
			t.Errorf("repositoryLabel(%q) = %q, want %q", remote, got, want)
		}
	}
	for _, test := range [][3]string{
		{"/p", "/p/coolship.toml", "./coolship.toml"},
		{"/p/apps/web", "/p/coolship.toml", "/p/coolship.toml"},
		{"/p", "/p/apps/coolship.toml", "./apps/coolship.toml"},
		{"", "/p/coolship.toml", "/p/coolship.toml"},
	} {
		if got := relativeBinding(test[0], test[1]); got != test[2] {
			t.Errorf("relativeBinding(%q, %q) = %q, want %q", test[0], test[1], got, test[2])
		}
	}
}

// TestHintsAndQuestionsStayInsideTheBlock checks the end of the block: the
// next steps and the closing question carry the bar.
func TestHintsAndQuestionsStayInsideTheBlock(t *testing.T) {
	var diagnostic bytes.Buffer
	streams := Streams{In: strings.NewReader("n\n"), Err: &diagnostic, Interactive: true, Block: testBlock(false, 90)}
	Hints(streams, "human", []NextStep{{"coolship domain set URL", "give the application a domain"}})
	if _, err := NewPrompter(streams).YesNo(context.Background(), "Deploy now?", false); err != nil {
		t.Fatal(err)
	}
	if want := "┃ Next:\n┃   coolship domain set URL  give the application a domain\n┃ Deploy now? [y/N] "; diagnostic.String() != want {
		t.Fatalf("got %q\nwant %q", diagnostic.String(), want)
	}
}

// TestStepsInsideABlockEraseExactlyTheirRows runs the live view behind the
// bar on a terminal too narrow for a row, through a pause and a close: each
// erase moves up over exactly the rows the frame drew, every row begins
// with the bar and is cut to the width, and nothing is left twice.
func TestStepsInsideABlockEraseExactlyTheirRows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bubble Tea maps newlines only off Windows; the recorded stream is replayed that way")
	}
	const width = 30
	file, err := os.Create(filepath.Join(t.TempDir(), "terminal"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	recorder := &terminalRecorder{}
	steps := NewSteps(Streams{Err: recorder, Interactive: true, Block: testBlock(false, width)}, "human",
		[]string{"Plan the application", "Create the application", "Write coolship.toml"})
	steps.terminal = file
	steps.display = &display{output: recorder, width: width, height: 40}
	var advance func()
	steps.clock, advance = fixedClock()
	within(t, func() error {
		steps.Start(0)
		advance()
		steps.Done(0, "a-name-long-enough-to-be-cut on home")
		steps.Pause()
		if _, err := recorder.Write([]byte("┃ Confirm [y/N]: y\n")); err != nil {
			return err
		}
		steps.Resume()
		steps.Start(1)
		advance()
		steps.Done(1, "site")
		steps.Done(2, "")
		steps.Close()
		return nil
	})
	stream := recorder.String()
	// The last frame drew the two rows left after the pause.
	before, up, _ := lastErase(t, stream)
	if up != 1 {
		t.Fatalf("the last erase moved up %d rows over a frame of 2: %q", up, before)
	}
	// The first frame drew all three, and the pause erased them from the top.
	if !strings.Contains(stream, "\r\x1b[2A\x1b[J┃ ✓ Plan the application") {
		t.Fatalf("the first erase did not move up 2 rows over a frame of 3: %q", stream)
	}
	var s screen
	if err := s.replay(stream); err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(s.text(), "\n")
	want := []string{"┃ ✓ Plan the application", "┃ Confirm [y/N]: y", "┃ ✓ Create the application", "┃ ✓ Write coolship.toml"}
	if len(rows) != len(want) {
		t.Fatalf("screen %q\nstream %q", s.text(), stream)
	}
	for i, row := range rows {
		cut := i != 1 // every step row is longer than the terminal is wide
		if !strings.HasPrefix(row, want[i]) || len([]rune(row)) > width || cut && (len([]rune(row)) != width || !strings.HasSuffix(row, "…")) {
			t.Fatalf("row %d is %q on a terminal of %d columns\nscreen %q", i, row, width, s.text())
		}
	}
}
