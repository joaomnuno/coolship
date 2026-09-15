package ui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/joaomnuno/coolship/internal/service"
)

// menuStatusMsg carries one read of the header. gen is the read's
// generation, so a read that finishes after a newer one started is dropped.
type menuStatusMsg struct {
	gen    int
	result service.StatusResult
	err    error
}

// menuRefreshMsg is the timer that reads the header again; gen ties it to
// the read that scheduled it, so pressing r never leaves two timers running.
type menuRefreshMsg struct{ gen int }

// menuKeys are the menu's bindings, shown in the footer by bubbles/help.
type menuKeys struct {
	run, filter, refresh, quit, clear key.Binding
	filtering                         bool
}

func newMenuKeys() menuKeys {
	return menuKeys{
		run:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "run")),
		filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		quit:    key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
		clear:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter")),
	}
}

func (k menuKeys) ShortHelp() []key.Binding {
	move := key.NewBinding(key.WithKeys("up"), key.WithHelp("↑/↓", "move"))
	if k.filtering {
		return []key.Binding{move, k.run, k.clear}
	}
	return []key.Binding{move, k.run, k.filter, k.refresh, k.quit}
}

func (k menuKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// menuModel is the Bubble Tea model behind RunMenu. It holds no terminal;
// tests drive it with messages and read its View.
type menuModel struct {
	ctx       context.Context
	groups    []MenuGroup
	load      func(context.Context) (service.StatusResult, error)
	notLinked func(error) bool
	refresh   time.Duration
	now       func() time.Time
	style     palette
	spinner   spinner.Model
	keys      menuKeys
	help      help.Model

	width, height int

	// readCtx is the context of the header read in flight, and cancelRead
	// ends it: a newer read, a quit, or a chosen verb supersedes it, so at
	// most one read runs at a time.
	readCtx    context.Context
	cancelRead context.CancelFunc

	gen     int
	loading bool
	loaded  bool
	status  service.StatusResult
	err     error

	filter    string
	filtering bool
	cursor    int // index into visible()
	offset    int // first list row drawn

	chosen *MenuVerb
}

func newMenuModel(ctx context.Context, style palette, options MenuOptions) menuModel {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	// The footer follows the stream's colour decision like every other line:
	// bold keys and dim descriptions with colour on, plain text with it off.
	h := help.New()
	h.Styles = help.Styles{}
	if style.profile == colorprofile.ANSI {
		h.Styles.ShortKey = style.styles[bold]
		h.Styles.ShortDesc = style.styles[dim]
		h.Styles.ShortSeparator = style.styles[dim]
		h.Styles.Ellipsis = style.styles[dim]
	}
	m := menuModel{
		ctx:       ctx,
		groups:    options.Groups,
		load:      options.Status,
		notLinked: options.NotLinked,
		refresh:   options.Refresh,
		now:       now,
		style:     style,
		spinner:   spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Cyan))),
		keys:      newMenuKeys(),
		help:      h,
	}
	m.startRead()
	return m
}

// startRead supersedes any read in flight with a new generation and a fresh
// context for the next one. The read itself is the command read returns.
func (m *menuModel) startRead() {
	m.stopReading()
	if m.load == nil {
		return
	}
	m.gen++
	m.readCtx, m.cancelRead = context.WithCancel(m.ctx)
	m.loading = true
}

// stopReading cancels the read in flight, if any.
func (m *menuModel) stopReading() {
	if m.cancelRead != nil {
		m.cancelRead()
		m.cancelRead = nil
	}
}

// reopen prepares the model to be shown again after an input prompt was
// abandoned: nothing chosen, and the header read again as the program starts.
func (m menuModel) reopen() menuModel {
	m.chosen = nil
	m.startRead()
	return m
}

func (m menuModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.read())
}

// read returns the command that reads the header for the current generation.
func (m menuModel) read() tea.Cmd {
	if m.load == nil || m.readCtx == nil {
		return nil
	}
	load, ctx, gen := m.load, m.readCtx, m.gen
	return func() tea.Msg {
		result, err := load(ctx)
		return menuStatusMsg{gen: gen, result: result, err: err}
	}
}

func (m menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
		m.scroll()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case menuStatusMsg:
		if msg.gen != m.gen || !m.loading {
			return m, nil
		}
		m.stopReading()
		m.loading, m.loaded = false, true
		m.status, m.err = msg.result, msg.err
		if msg.err != nil {
			m.status = service.StatusResult{}
		}
		if m.refresh <= 0 || m.isNotLinked() {
			return m, nil
		}
		gen := m.gen
		return m, tea.Tick(m.refresh, func(time.Time) tea.Msg { return menuRefreshMsg{gen: gen} })
	case menuRefreshMsg:
		if msg.gen != m.gen || m.loading {
			return m, nil
		}
		return m.reread()
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// reread starts a new read of the header, cancelling any in flight.
func (m menuModel) reread() (tea.Model, tea.Cmd) {
	if m.load == nil {
		return m, nil
	}
	m.startRead()
	return m, m.read()
}

// quit ends the program and the read in flight with it.
func (m menuModel) quit() (tea.Model, tea.Cmd) {
	m.stopReading()
	m.loading = false
	return m, tea.Quit
}

func (m menuModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		if m.filtering {
			m.filtering, m.filter = false, ""
			m.cursor = 0
			m.scroll()
			return m, nil
		}
		return m.quit()
	case "up", "ctrl+p":
		m.move(-1)
		return m, nil
	case "down", "ctrl+n":
		m.move(1)
		return m, nil
	case "enter":
		visible := m.visible()
		if len(visible) == 0 {
			return m, nil
		}
		verb := visible[m.cursor].verb
		m.chosen = &verb
		return m.quit()
	case "backspace":
		if m.filtering {
			if m.filter != "" {
				_, size := utf8.DecodeLastRuneInString(m.filter)
				m.filter = m.filter[:len(m.filter)-size]
			}
			if m.filter == "" {
				m.filtering = false
			}
			m.cursor = 0
			m.scroll()
		}
		return m, nil
	}
	if !m.filtering {
		switch msg.String() {
		case "k":
			m.move(-1)
			return m, nil
		case "j":
			m.move(1)
			return m, nil
		case "q":
			return m.quit()
		case "r":
			return m.reread()
		case "/":
			m.filtering = true
			return m, nil
		}
	}
	text := msg.Key().Text
	if text == "" || msg.Key().Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
		return m, nil
	}
	// Any other printable key starts the filter with itself; while
	// filtering, every printable key, j, k, q, and r included, is text.
	m.filtering = true
	m.filter += text
	m.cursor = 0
	m.scroll()
	return m, nil
}

func (m *menuModel) move(delta int) {
	count := len(m.visible())
	if count == 0 {
		return
	}
	m.cursor = (m.cursor + delta + count) % count
	m.scroll()
}

// menuEntry is one verb the list shows, with the group it belongs to.
type menuEntry struct {
	group int
	verb  MenuVerb
}

// visible lists the verbs that match the filter, in declared order. A verb
// matches when its path contains the filter, ignoring case.
func (m menuModel) visible() []menuEntry {
	needle := strings.ToLower(strings.TrimSpace(m.filter))
	var entries []menuEntry
	for g, group := range m.groups {
		for _, verb := range group.Verbs {
			if needle == "" || strings.Contains(strings.ToLower(verb.Name()), needle) {
				entries = append(entries, menuEntry{group: g, verb: verb})
			}
		}
	}
	return entries
}

func (m menuModel) isNotLinked() bool {
	return m.err != nil && m.notLinked != nil && m.notLinked(m.err)
}

// headerLines is how many rows the title, the three header lines, and the
// line above the list (blank, or the filter being typed) take, and
// footerLines the blank line and the help. View cuts every line to the
// terminal's width, so none of them wraps into a second row.
const (
	headerLines = 5
	footerLines = 2
)

// compact reports a terminal too short for the header and footer with even
// one list row under them. The view then drops both and draws only the list,
// and the filter line while one is typed.
func (m menuModel) compact() bool {
	return m.height > 0 && m.height < headerLines+footerLines+1
}

// listHeight is how many list rows fit; zero means the height is unknown and
// the whole list is drawn.
func (m menuModel) listHeight() int {
	switch {
	case m.height <= 0:
		return 0
	case m.compact() && m.filtering:
		return max(1, m.height-1)
	case m.compact():
		return m.height
	}
	return m.height - headerLines - footerLines
}

// scroll moves the window over the list so the cursor's row, and the title
// of its group when the cursor is on the group's first verb, stay in view.
func (m *menuModel) scroll() {
	rows, cursorRow := m.rows()
	height := m.listHeight()
	if height == 0 || cursorRow < 0 {
		m.offset = 0
		return
	}
	top := cursorRow
	if cursorRow > 0 && rows[cursorRow-1].title {
		top = cursorRow - 1
	}
	if top < m.offset {
		m.offset = top
	}
	if cursorRow >= m.offset+height {
		m.offset = cursorRow - height + 1
	}
	m.offset = max(0, min(m.offset, len(rows)-height))
}

// menuRow is one line of the list: a group title or a verb.
type menuRow struct {
	title bool
	text  string
	entry int
}

// rows lays out the list and returns the row the cursor is on, or -1.
func (m menuModel) rows() ([]menuRow, int) {
	visible := m.visible()
	var rows []menuRow
	cursorRow := -1
	lastGroup := -1
	for i, entry := range visible {
		if entry.group != lastGroup {
			rows = append(rows, menuRow{title: true, text: m.groups[entry.group].Title, entry: -1})
			lastGroup = entry.group
		}
		if i == m.cursor {
			cursorRow = len(rows)
		}
		rows = append(rows, menuRow{text: entry.verb.Name(), entry: i})
	}
	return rows, cursorRow
}

func (m menuModel) View() tea.View {
	if m.compact() {
		var out strings.Builder
		if m.filtering && m.height > 1 {
			out.WriteString(m.style.apply(cyan, "/") + " " + singleLine(m.filter) + m.style.apply(dim, "▏") + "\n")
		}
		out.WriteString(m.list())
		view := tea.NewView(m.fit(strings.TrimSuffix(out.String(), "\n")))
		view.AltScreen = true
		return view
	}
	var out strings.Builder
	out.WriteString(m.style.apply(bold, "coolship ui") + "  " + m.style.apply(yellow, "experimental") + "\n")
	for _, line := range m.header() {
		out.WriteString(line + "\n")
	}
	if m.filtering {
		out.WriteString(m.style.apply(cyan, "/") + " " + singleLine(m.filter) + m.style.apply(dim, "▏") + "\n")
	} else {
		out.WriteString("\n")
	}
	out.WriteString(m.list())
	out.WriteString("\n")
	m.keys.filtering = m.filtering
	out.WriteString(m.help.View(m.keys))
	view := tea.NewView(m.fit(out.String()))
	view.AltScreen = true
	return view
}

// fit cuts every line to the terminal's width, escape sequences aside, so
// each line takes exactly one row and the row budget holds. An unknown width
// leaves the lines whole.
func (m menuModel) fit(content string) string {
	if m.width <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, m.width, "…")
	}
	return strings.Join(lines, "\n")
}

// header is three lines: the target, the status, and the last deployment,
// or what stands in for them while the first read is on its way, when the
// directory is not linked, or when the read failed.
func (m menuModel) header() []string {
	lines := []string{"", "", ""}
	switch {
	case !m.loaded && m.loading:
		lines[0] = m.spinner.View() + " " + m.style.apply(dim, "Reading application status")
	case m.isNotLinked():
		lines[0] = m.style.apply(yellow, "○") + " " + m.style.apply(bold, "not linked")
		lines[1] = m.style.apply(dim, "Choose link to bind this directory to an application, or init to create one.")
	case m.err != nil:
		lines[0] = m.style.apply(red, "✗") + " " + m.style.apply(redBold, "Status unavailable")
		lines[1] = m.style.apply(dim, singleLine(m.err.Error()))
		lines[2] = m.style.apply(dim, "Press r to try again.")
	case m.loaded:
		target := m.status.Target
		name := singleLine(target.Application)
		if target.Target != "" && target.Target != "default" {
			name += " " + m.style.apply(dim, "("+singleLine(target.Target)+")")
		}
		sep := m.style.apply(dim, " · ")
		lines[0] = m.style.apply(bold, name) + "  " + singleLine(target.Environment) + sep +
			m.style.apply(dim, "project ") + singleLine(target.Project) + sep +
			m.style.apply(dim, "context ") + singleLine(target.Instance)
		status := singleLine(m.status.Status)
		if status == "" {
			status = "unknown"
		}
		lines[1] = applicationState(m.style, true, status)
		if m.status.URL != "" {
			lines[1] += "  " + m.style.apply(dim, singleLine(m.status.URL))
		}
		lines[2] = m.style.key("Last deployment") + " " + m.lastDeployment()
	}
	if m.loaded && m.loading {
		lines[0] += "  " + m.spinner.View()
	}
	return lines
}

// lastDeployment is the status, the short commit, and how long ago the
// newest deployment was queued.
func (m menuModel) lastDeployment() string {
	last := m.status.LastDeployment
	if last == nil {
		for _, warning := range m.status.Warnings {
			if reason, ok := strings.CutPrefix(warning, service.HistoryUnreadableWarning); ok {
				return m.style.apply(yellow, "unavailable") + "  " + m.style.apply(dim, singleLine(reason))
			}
		}
		return m.style.apply(dim, "none yet")
	}
	status := singleLine(last.Status)
	line := m.style.apply(deploymentStatus(status), deploymentGlyph(status)+" "+status)
	if commit := shortCommit(last.Commit); commit != "" {
		line += "  " + commit
	}
	if last.CreatedAt != "" {
		if when := relativeTime(last.CreatedAt, m.now()); when != "" {
			line += "  " + m.style.apply(dim, when)
		}
	}
	return line
}

// list draws the visible window of rows. The selected verb has a cyan
// pointer; a verb that asks for input before it runs ends in an ellipsis.
func (m menuModel) list() string {
	rows, cursorRow := m.rows()
	if len(rows) == 0 {
		return m.style.apply(dim, fmt.Sprintf("No command matches %q", singleLine(m.filter))) + "\n"
	}
	visible := m.visible()
	nameWidth := 0
	for _, group := range m.groups {
		for _, verb := range group.Verbs {
			nameWidth = max(nameWidth, len(verb.Name())+1)
		}
	}
	start, end := 0, len(rows)
	if height := m.listHeight(); height > 0 && len(rows) > height {
		start = m.offset
		end = min(len(rows), start+height)
	}
	var out strings.Builder
	for i := start; i < end; i++ {
		row := rows[i]
		if row.title {
			out.WriteString(m.style.apply(bold, row.text) + "\n")
			continue
		}
		verb := visible[row.entry].verb
		name := verb.Name()
		if len(verb.Inputs) > 0 {
			name += "…"
		}
		padding := strings.Repeat(" ", max(1, nameWidth-utf8.RuneCountInString(name)+2))
		short := singleLine(verb.Short)
		if i == cursorRow {
			out.WriteString(m.style.apply(cyan, "› ") + m.style.apply(cyan, name) + padding + short + "\n")
			continue
		}
		out.WriteString("  " + name + padding + m.style.apply(dim, short) + "\n")
	}
	return out.String()
}

// deploymentGlyph marks a deployment status the way the checklist does.
func deploymentGlyph(status string) string {
	switch status {
	case "finished":
		return "✓"
	case "failed", "cancelled-by-user":
		return "✗"
	}
	return "●"
}
