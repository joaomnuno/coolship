package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/joaomnuno/coolship/internal/alias"
	"github.com/joaomnuno/coolship/internal/service"
)

// Renderer keeps requested output separate from progress and diagnostics.
type Renderer struct {
	streams Streams
	format  string
	out     palette
	err     palette
	// now is the clock relative times and running durations are read from.
	now func() time.Time
}

func NewRenderer(streams Streams, format string) *Renderer {
	streams = streams.Normalized()
	return &Renderer{streams: streams, format: format, out: streams.outPalette(), err: streams.errPalette(), now: time.Now}
}

// namesOnly reports whether stdout shows names without UUIDs: a terminal at
// normal verbosity. --verbose and --debug add the UUIDs back, and a pipe
// keeps them at every level so its text never changes under a script.
func (r *Renderer) namesOnly() bool {
	return r.streams.OutTerminal && r.streams.level() == VerbosityNormal
}

// errNamesOnly is namesOnly for the progress lines on stderr. With --format
// json the lines keep the deployment UUID even on a terminal, so a script
// can match them to the JSON result.
func (r *Renderer) errNamesOnly() bool {
	return r.format != "json" && r.streams.ErrTerminal && r.streams.level() == VerbosityNormal
}

// named labels one resource: its name alone when names only, otherwise the
// name with its UUID. Every result names a single application or key, so no
// two labels on a screen can share a name and need the UUID to tell apart.
func named(name, uuid string, namesOnly bool) string {
	name, uuid = singleLine(name), singleLine(uuid)
	switch {
	case uuid == "":
		return name
	case name == "":
		return uuid
	case namesOnly:
		return name
	}
	return name + " (" + uuid + ")"
}

// deploymentLabel identifies a deployment, which has no name. Names-only
// output shows the short ID that deployments lists and cancel accepts;
// otherwise the full UUID.
func deploymentLabel(uuid string, namesOnly bool) string {
	if namesOnly {
		return shortID(uuid)
	}
	return singleLine(uuid)
}

// applicationState renders an application status on a stream: on a terminal
// a glyph and the status in the colour of its state, and plain elsewhere.
func applicationState(p palette, terminal bool, status string) string {
	status = singleLine(status)
	if !terminal || status == "" {
		return status
	}
	return p.apply(applicationStatus(status), "● "+status)
}

// relativeTime says how long ago a server timestamp was, coarsely, the way a
// person reads a history; past a month it is the date. An unreadable value,
// or one well in the future, is shown as localTime shows it.
func relativeTime(value string, now time.Time) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return localTime(value)
	}
	ago := now.Sub(parsed)
	switch {
	case ago < -time.Minute:
		return localTime(value)
	case ago < time.Minute:
		return "just now"
	case ago < time.Hour:
		return strconv.Itoa(int(ago/time.Minute)) + " min ago"
	case ago < 24*time.Hour:
		return strconv.Itoa(int(ago/time.Hour)) + " h ago"
	case ago < 48*time.Hour:
		return "1 day ago"
	case ago < 30*24*time.Hour:
		return strconv.Itoa(int(ago/(24*time.Hour))) + " days ago"
	}
	return parsed.Local().Format("2006-01-02")
}

// when renders a timestamp on stdout: relative when names only, exact
// otherwise.
func (r *Renderer) when(value string) string {
	if r.namesOnly() {
		return relativeTime(value, r.now())
	}
	return localTime(value)
}

func (r *Renderer) Status(result service.StatusResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	if err := r.target(result.Target); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n", r.out.key("Status"), applicationState(r.out, r.streams.OutTerminal, result.Status)); err != nil {
		return err
	}
	if result.URL != "" {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n", r.out.key("URL"), singleLine(result.URL)); err != nil {
			return err
		}
	}
	if last := result.LastDeployment; last != nil {
		status := singleLine(last.Status)
		// A pipe keeps the short ID it always had; a terminal shows the
		// short ID at normal verbosity and the full UUID above it.
		id := shortID(last.UUID)
		if r.streams.OutTerminal {
			id = deploymentLabel(last.UUID, r.namesOnly())
		}
		line := id + " " + r.out.apply(deploymentStatus(status), status)
		if commit := shortCommit(last.Commit); commit != "" {
			line += " (" + commit + ")"
		}
		if when := r.when(last.CreatedAt); when != "" {
			line += " " + when
		}
		if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n", r.out.key("Last deployment"), line); err != nil {
			return err
		}
	}
	return nil
}

// shortID abbreviates a deployment UUID the way the history table does; a
// prefix is enough to tell rows apart and to paste into cancel.
func shortID(uuid string) string {
	uuid = singleLine(uuid)
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}

// shortCommit abbreviates a sha; HEAD, the placeholder Coolify records until
// the job resolves the commit, stays as it is.
func shortCommit(commit string) string {
	commit = singleLine(commit)
	if len(commit) > 7 && isHex(commit) {
		return commit[:7]
	}
	return commit
}

func isHex(value string) bool {
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return value != ""
}

// localTime renders one of the server's timestamps in the viewer's zone; an
// unreadable value is shown as the server sent it rather than dropped.
func localTime(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return singleLine(value)
	}
	return parsed.Local().Format("2006-01-02 15:04:05")
}

// duration reports how long a deployment took, or has been running. Coolify
// records finished_at only when the deployment job wraps up, so a deployment
// cancelled before the job reached it has none; such a row has no duration
// rather than one that grows with the clock.
func duration(status, created, finished string, now time.Time) string {
	start, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return ""
	}
	end := now
	if finished != "" {
		end, err = time.Parse(time.RFC3339Nano, finished)
		if err != nil {
			return ""
		}
	} else if status != "queued" && status != "in_progress" {
		return ""
	}
	elapsed := end.Sub(start).Round(time.Second)
	if elapsed < 0 {
		return ""
	}
	return elapsed.String()
}

// clockDuration is duration on the stage checklist's clock, 0:24 or 1:02:03,
// so a terminal shows one way of writing a length of time.
func clockDuration(status, created, finished string, now time.Time) string {
	start, err := time.Parse(time.RFC3339Nano, created)
	if err != nil || duration(status, created, finished, now) == "" {
		return ""
	}
	end := now
	if finished != "" {
		end, _ = time.Parse(time.RFC3339Nano, finished)
	}
	return FormatElapsed(end.Sub(start).Round(time.Second))
}

// dropOrder lists the deployments table's columns in the order a narrow
// terminal gives them up: the type, the commit, the duration, then the time.
// The identifier and the status always stay.
var dropOrder = []int{3, 2, 5, 4}

// fitColumns picks the columns of a table whose widths fit in width columns,
// with two spaces between them. Zero width is unknown and keeps them all.
func fitColumns(widths []int, width int) []int {
	keep := make([]bool, len(widths))
	for i := range keep {
		keep[i] = true
	}
	total := func() int {
		sum, count := 0, 0
		for i, kept := range keep {
			if kept {
				sum += widths[i]
				count++
			}
		}
		return sum + 2*max(count-1, 0)
	}
	for _, column := range dropOrder {
		if width <= 0 || total() <= width {
			break
		}
		keep[column] = false
	}
	var shown []int
	for i, kept := range keep {
		if kept {
			shown = append(shown, i)
		}
	}
	return shown
}

// Deployments renders the history as a table, newest first, without build
// logs in any format.
func (r *Renderer) Deployments(result service.DeploymentsResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	if len(result.Deployments) == 0 {
		_, err := fmt.Fprintf(r.streams.Out, "%s has no deployments\n", singleLine(result.Target.Application))
		return err
	}
	if _, err := fmt.Fprintf(r.streams.Out, "Deployments of %s (%d of %d)\n", singleLine(result.Target.Application), len(result.Deployments), result.Total); err != nil {
		return err
	}
	// A pipe keeps the table it always had. A terminal shows the short ID
	// under ID, which cancel accepts, at normal verbosity, and the full UUID
	// above it; times are relative at normal and durations read like the
	// stage checklist's clock.
	terminal, namesOnly := r.streams.OutTerminal, r.namesOnly()
	idHeader := "UUID"
	if namesOnly {
		idHeader = "ID"
	}
	rows := [][]string{{idHeader, "STATUS", "COMMIT", "TYPE", "CREATED", "DURATION"}}
	now := r.now()
	for _, deployment := range result.Deployments {
		kind := singleLine(deployment.Kind)
		if deployment.PullRequest > 0 {
			kind = "preview #" + strconv.Itoa(deployment.PullRequest)
		}
		id := shortID(deployment.UUID)
		took := duration(deployment.Status, deployment.CreatedAt, deployment.FinishedAt, now)
		if terminal {
			id = deploymentLabel(deployment.UUID, namesOnly)
			took = clockDuration(deployment.Status, deployment.CreatedAt, deployment.FinishedAt, now)
		}
		rows = append(rows, []string{id, singleLine(deployment.Status), shortCommit(deployment.Commit), kind,
			r.when(deployment.CreatedAt), took})
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	shown := fitColumns(widths, r.streams.Width)
	if !terminal {
		shown = []int{0, 1, 2, 3, 4, 5}
	}
	for index, row := range rows {
		var line strings.Builder
		for position, i := range shown {
			cell := row[i]
			if position > 0 {
				line.WriteString("  ")
			}
			padding := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell))
			if index > 0 && i == 1 {
				cell = r.out.apply(deploymentStatus(cell), cell)
			}
			line.WriteString(cell)
			if position < len(shown)-1 {
				line.WriteString(padding)
			}
		}
		if _, err := fmt.Fprintln(r.streams.Out, strings.TrimRight(line.String(), " ")); err != nil {
			return err
		}
	}
	return nil
}

// Stop renders the final result only; warnings already reached stderr as
// events, as with Deploy.
func (r *Renderer) Stop(result service.StopResult) error {
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	_, err := fmt.Fprintf(r.streams.Out, "%s %s\n%s %s\n",
		r.out.key("Application"), named(result.Target.Application, result.Target.ApplicationUUID, r.namesOnly()),
		r.out.key("Status"), applicationState(r.out, r.streams.OutTerminal, result.Status))
	return err
}

func (r *Renderer) Cancel(result service.CancelResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	status := singleLine(result.Status)
	_, err := fmt.Fprintf(r.streams.Out, "%s %s\n%s %s\n%s %s\n",
		r.out.key("Deployment"), deploymentLabel(result.DeploymentUUID, r.namesOnly()),
		r.out.key("Application"), named(result.Target.Application, result.Target.ApplicationUUID, r.namesOnly()),
		r.out.key("Status"), r.out.apply(deploymentStatus(status), status))
	return err
}

// Link reports the binding. A run drawn inside a Block already showed every
// name in its checklist, so it ends with one line saying the application is
// linked; every other run gets the full result it always did.
func (r *Renderer) Link(result service.LinkResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	if r.brief() {
		return r.linked(result.Target.Application, "", true)
	}
	if _, err := fmt.Fprintf(r.streams.Out, "Linked project in %s\n", singleLine(result.Path)); err != nil {
		return err
	}
	return r.target(result.Target)
}

// Init reports the creation, then the binding like Link does. A deployment
// that followed is rendered like Deploy's result; its progress already went
// to stderr as events. When only a deploy key was created, its public half
// is printed once with what to do next; the private half never reaches
// this layer.
//
// A run drawn inside a Block approved the plan a moment ago, warnings
// included, and watched the checklist, so it ends with what is new: one line
// naming the application and its URL. Every other run gets the full result
// it always did; there the warnings the confirmation already showed are not
// printed a second time.
func (r *Renderer) Init(result service.InitResult, confirmed bool) error {
	warnings := result.Warnings
	if confirmed {
		warnings = warnings[min(len(result.Plan.Warnings), len(warnings)):]
	} else if private := result.Plan.Private; private != "" && r.format != "json" && r.brief() {
		// A private source was just chosen for this reason; the probe's
		// own words are for --verbose.
		warnings = slices.DeleteFunc(slices.Clone(warnings), func(warning string) bool { return warning == private })
	}
	if err := r.warnings(warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	plan := result.Plan
	if r.brief() && result.Target.ApplicationUUID != "" {
		// A deployment's progress ended the block, so its result stands
		// outside it, as deploy's own does.
		if err := r.linked(plan.Name, result.URL, result.Deployment == nil); err != nil {
			return err
		}
		if result.Deployment != nil {
			return r.Deploy(*result.Deployment)
		}
		return nil
	}
	if key := result.DeployKey; key != nil && result.Target.ApplicationUUID == "" {
		_, err := fmt.Fprintf(r.streams.Out, "Created deploy key %s on %s\n%s\n%s\n\nAdd it to %s as a read-only deploy key, then create the application with:\n  coolship init --source deploy-key --deploy-key %s --repo %s\n",
			named(key.Name, key.UUID, r.namesOnly()), singleLine(plan.Instance), r.out.key("Public key"), singleLine(key.PublicKey),
			singleLine(key.Repository), shellQuote(singleLine(key.Name)), shellQuote(singleLine(key.Repository)))
		return err
	}
	buildPack := singleLine(plan.BuildPack)
	if plan.Port > 0 {
		buildPack += fmt.Sprintf(", port %d", plan.Port)
	}
	if plan.Static {
		buildPack += ", static"
	}
	if _, err := fmt.Fprintf(r.streams.Out, "Created application %s from %s at %s\n%s %s\n",
		named(plan.Name, result.Target.ApplicationUUID, r.namesOnly()), singleLine(plan.Repository), singleLine(plan.Branch),
		r.out.key("Build pack"), buildPack); err != nil {
		return err
	}
	for _, detail := range buildDetails(plan) {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n", r.out.key(detail[0]), detail[1]); err != nil {
			return err
		}
	}
	switch plan.Source {
	case service.SourceGitHubApp:
		if _, err := fmt.Fprintf(r.streams.Out, "%s GitHub App %s\n", r.out.key("Source"), singleLine(plan.GitHubApp)); err != nil {
			return err
		}
	case service.SourceDeployKey:
		if _, err := fmt.Fprintf(r.streams.Out, "%s deploy key %s\n", r.out.key("Source"), singleLine(plan.DeployKey)); err != nil {
			return err
		}
	}
	if result.URL != "" {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n", r.out.key("URL"), singleLine(result.URL)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(r.streams.Out, "Linked project in %s\n", singleLine(plan.Path)); err != nil {
		return err
	}
	if err := r.target(result.Target); err != nil {
		return err
	}
	if result.Deployment != nil {
		return r.Deploy(*result.Deployment)
	}
	return nil
}

// Deploy renders the final result only. The same warnings already reached
// stderr as events before submission, so repeating them here would duplicate
// every warning in both formats.
func (r *Renderer) Deploy(result service.DeployResult) error {
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n", r.out.key("Deployment"), deploymentLabel(result.DeploymentUUID, r.namesOnly())); err != nil {
		return err
	}
	if result.PullRequest > 0 {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %d\n", r.out.key("Pull request"), result.PullRequest); err != nil {
			return err
		}
	}
	status := singleLine(result.Status)
	if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n%s %s\n",
		r.out.key("Application"), named(result.Target.Application, result.Target.ApplicationUUID, r.namesOnly()),
		r.out.key("Status"), r.out.apply(deploymentStatus(status), status)); err != nil {
		return err
	}
	// The URL is the last line, so it is what the eye lands on: the
	// application in green when it is live, its Coolify page plain otherwise.
	if result.URL == "" {
		return nil
	}
	style := plain
	if result.URLKind == "application" {
		style = green
	}
	_, err := fmt.Fprintln(r.streams.Out, r.out.apply(style, singleLine(result.URL)))
	return err
}

// DeploymentPage names, on stderr, the Coolify page of a deployment that did
// not finish, so the diagnostic that follows has somewhere to point.
func (r *Renderer) DeploymentPage(url string) error {
	_, err := fmt.Fprintf(r.streams.Err, "%s %s\n", r.err.key("Deployment page"), singleLine(url))
	return err
}

// DeploymentEvent renders observation and build logs on stderr in both formats.
func (r *Renderer) DeploymentEvent(event service.Event) error {
	return r.deploymentEvent(event, "")
}

// deploymentEvent is DeploymentEvent with a suffix, such as a duration,
// appended to stage and deployment status lines only.
func (r *Renderer) deploymentEvent(event service.Event, suffix string) error {
	if event.Type == "warning" {
		return r.warnings([]string{event.Message})
	}
	if event.Logs != "" {
		return writeLogs(r.streams.Err, event.Logs)
	}
	message := singleLine(event.Message)
	switch {
	case event.Type == "stage":
		// A stage line reads like a status line: what, then its state.
		message = "Stage " + singleLine(event.Stage) + ": " + singleLine(event.Status) + suffix
	case event.Type == "application":
		// Stop reports the server's receipt once, then each status it observes.
		if message == "" {
			message = "Application status: " + applicationState(r.err, r.streams.ErrTerminal, event.Status)
		}
	case message == "" && event.Status != "":
		status := singleLine(event.Status)
		// The result names the deployment once; on a terminal at normal
		// verbosity each progress line need not repeat its UUID.
		subject := "Deployment " + singleLine(event.DeploymentUUID)
		if r.errNamesOnly() {
			subject = "Deployment status"
		}
		message = subject + ": " + r.err.apply(deploymentStatus(status), status) + suffix
	}
	if message == "" {
		return nil
	}
	_, err := fmt.Fprintln(r.streams.Err, message)
	return err
}

// LogEvent uses NDJSON for machine output, including reset and gap warnings.
func (r *Renderer) LogEvent(event service.Event) error {
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(event)
	}
	if event.Type == "warning" {
		return r.warnings([]string{event.Message})
	}
	if event.Type == "logs" {
		return writeLogs(r.streams.Out, event.Logs)
	}
	return r.DeploymentEvent(event)
}

func (r *Renderer) target(target service.TargetInfo) error {
	if target.Target != "" && target.Target != "default" {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n", r.out.key("Target"), singleLine(target.Target)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(r.streams.Out,
		"%s %s\n%s %s\n%s %s\n%s %s\n",
		r.out.key("Application"), named(target.Application, target.ApplicationUUID, r.namesOnly()),
		r.out.key("Environment"), singleLine(target.Environment),
		r.out.key("Project"), singleLine(target.Project),
		r.out.key("Context"), singleLine(target.Instance))
	return err
}

// Warn writes one warning to stderr in the style every result's warnings use.
func (r *Renderer) Warn(message string) error { return r.warnings([]string{message}) }

func (r *Renderer) warnings(warnings []string) error {
	for _, warning := range warnings {
		if err := r.streams.Block.println(r.streams.Err, r.err.apply(yellow, "Warning:")+" "+singleLine(warning)); err != nil {
			return err
		}
	}
	return nil
}

// brief reports whether a result that a Block's checklist already showed is
// cut down to what is new: only when that block was drawn and stdout is the
// terminal it was drawn on. A piped stdout keeps the full result.
func (r *Renderer) brief() bool {
	return r.streams.Block.Active() && r.streams.OutTerminal
}

// linked is the line a brief init or link ends with, inside the block
// unless something else already ended it.
func (r *Renderer) linked(application, url string, inBlock bool) error {
	line := r.out.apply(bold, singleLine(application)) + " is linked."
	if url != "" {
		line = r.out.apply(bold, singleLine(application)) + " is linked: " + singleLine(url)
	}
	var block *Block
	if inBlock {
		block = r.streams.Block
	}
	return block.println(r.streams.Out, line)
}

func writeLogs(w io.Writer, logs string) error {
	if logs == "" {
		return nil
	}
	if _, err := io.WriteString(w, logs); err != nil {
		return err
	}
	if !strings.HasSuffix(logs, "\n") {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}

// Resource labels and diagnostics cannot introduce terminal controls or lines.
// Requested application/build logs retain their original text.
func singleLine(value string) string {
	var out strings.Builder
	for _, char := range value {
		switch char {
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if unicode.IsControl(char) || unicode.In(char, unicode.Bidi_Control) {
				fmt.Fprintf(&out, `\u%04x`, char)
			} else {
				out.WriteRune(char)
			}
		}
	}
	return out.String()
}

// shellQuote makes a value safe to paste into a POSIX shell command line, so
// a repository URL or key name with spaces or shell metacharacters does not
// split into extra arguments when a suggested command is copied verbatim.
// A value made only of characters that never need escaping is left bare.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	bare := true
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		case strings.ContainsRune("_-./:@+", char):
		default:
			bare = false
		}
		if !bare {
			break
		}
	}
	if bare {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// Open prints the resolved URL on stdout so it can be piped; launching is
// reported separately by the command on stderr.
func (r *Renderer) Open(result service.OpenResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	_, err := fmt.Fprintln(r.streams.Out, singleLine(result.URL))
	return err
}

func (r *Renderer) Unlink(result service.UnlinkResult) error {
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	_, err := fmt.Fprintf(r.streams.Out, "Unlinked %s\n", singleLine(result.Path))
	return err
}

// Delete reports the application that is gone. Warnings, including a binding
// that could not be removed, were already emitted as events, so they are not
// repeated here.
func (r *Renderer) Delete(result service.DeleteResult) error {
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	if _, err := fmt.Fprintf(r.streams.Out, "Deleted %s from %s\n",
		named(result.Target.Application, result.Target.ApplicationUUID, r.namesOnly()), singleLine(result.Target.Project)); err != nil {
		return err
	}
	if result.KeepVolumes {
		if _, err := fmt.Fprintln(r.streams.Out, "Its volumes were kept."); err != nil {
			return err
		}
	}
	if result.Unlinked != "" {
		if _, err := fmt.Fprintf(r.streams.Out, "Unlinked %s\n", singleLine(result.Unlinked)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) Config(result service.ConfigResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	b := result.Binding
	rows := [][2]string{
		{"Configuration", result.ConfigPath},
		{"Git root", result.GitRoot},
		{"Target", result.Target},
		{"Application root", result.AppRoot},
		{"Context", b.Context},
		{"Project", describeSelector(b.Project, b.ProjectUUID)},
		{"Environment", describeSelector(b.Environment, b.EnvironmentUUID)},
		{"Application", describeSelector(b.Application, b.ApplicationUUID)},
		{"Credentials", describeCredentials(result)},
		{"Instance", describeInstance(result)},
		{"Preferences", describePreferences(result.Preferences)},
	}
	for _, key := range slices.Sorted(maps.Keys(result.Overrides)) {
		rows = append(rows, [2]string{"Override " + key, result.Overrides[key]})
	}
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		// Pad on the plain label so styling never shifts the value column.
		padding := strings.Repeat(" ", max(0, 17-utf8.RuneCountInString(row[0]+":")))
		if _, err := fmt.Fprintf(r.streams.Out, "%s%s %s\n", r.out.key(row[0]), padding, singleLine(row[1])); err != nil {
			return err
		}
	}
	return nil
}

// describePreferences shows the path and, after it, why there is nothing
// more to show or the keys the file sets.
func describePreferences(report *service.PreferencesReport) string {
	if report == nil {
		return ""
	}
	switch {
	case report.Path == "":
		return "(not read: " + report.Error + ")"
	case report.Error != "":
		return report.Path + " (ignored: " + report.Error + ")"
	case !report.Present:
		return report.Path + " (absent)"
	}
	var set []string
	if report.Verbosity != "" {
		set = append(set, "verbosity "+report.Verbosity)
	}
	if report.BuildLogs != nil {
		set = append(set, "build logs "+map[bool]string{true: "on", false: "off"}[*report.BuildLogs])
	}
	if report.Color != "" {
		set = append(set, "color "+report.Color)
	}
	if report.UpdateCheck != nil {
		set = append(set, "update check "+map[bool]string{true: "on", false: "off"}[*report.UpdateCheck])
	}
	if report.Hints != nil {
		set = append(set, "hints "+map[bool]string{true: "on", false: "off"}[*report.Hints])
	}
	if len(set) == 0 {
		return report.Path + " (no keys set)"
	}
	return report.Path + " (" + strings.Join(set, ", ") + ")"
}

func describeSelector(name, uuid string) string {
	switch {
	case name != "" && uuid != "":
		return name + " (" + uuid + ")"
	case uuid != "":
		return uuid
	default:
		return name
	}
}

func describeCredentials(result service.ConfigResult) string {
	if result.CredentialSource == "environment" {
		return "COOLSHIP_URL and COOLSHIP_TOKEN"
	}
	return result.CredentialPath
}

func describeInstance(result service.ConfigResult) string {
	if result.Instance == "" {
		return ""
	}
	return result.Instance + " at " + result.InstanceURL
}

// Doctor renders one line per check with an ASCII marker, so the output reads
// the same in every terminal and in CI logs; color only tints the marker.
func (r *Renderer) Doctor(result service.DoctorResult) error {
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	markers := map[string]struct {
		marker, padding string
		style           look
	}{
		"ok":      {"[ok]", "  ", green},
		"warning": {"[warn]", "", yellow},
		"failed":  {"[FAIL]", "", redBold},
		"skipped": {"[skip]", "", dim},
	}
	for _, check := range result.Checks {
		known, ok := markers[check.Status]
		marker := "[" + singleLine(check.Status) + "]"
		if ok {
			marker = r.out.apply(known.style, known.marker) + known.padding
		}
		line := marker + " " + singleLine(check.Name)
		detail := singleLine(check.Detail)
		// On a terminal the application check is rebuilt from its parts,
		// with the UUID only above normal and the status in its colour; a
		// pipe and JSON keep the detail as the service wrote it.
		if app := check.Application; app != nil && r.streams.OutTerminal {
			detail = named(app.Name, app.UUID, r.namesOnly()) + " is " + applicationState(r.out, true, app.Status)
		}
		if detail != "" {
			line += ": " + detail
		}
		if _, err := fmt.Fprintln(r.streams.Out, line); err != nil {
			return err
		}
	}
	return nil
}

// mask hides a value while keeping its presence visible.
func mask(value string, reveal bool) string {
	if reveal {
		return singleLine(value)
	}
	if value == "" {
		return "(empty)"
	}
	return "********"
}

func (r *Renderer) EnvDiff(result service.EnvDiffResult, reveal bool) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		if !reveal {
			result = maskDiff(result)
		}
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	if _, err := fmt.Fprintf(r.streams.Out, "Comparing %s with %s variables of %s\n", singleLine(result.File), result.Scope, singleLine(result.Target.Application)); err != nil {
		return err
	}
	if result.Clean() && len(result.Withheld) == 0 {
		_, err := fmt.Fprintf(r.streams.Out, "No differences (%d unchanged)\n", result.Unchanged)
		return err
	}
	for _, change := range result.Added {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %s=%s  (local only; push creates it)\n", r.out.apply(green, "+"), change.Key, mask(change.Local, reveal)); err != nil {
			return err
		}
	}
	for _, change := range result.Changed {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %s: local %s, remote %s\n", r.out.apply(yellow, "~"), change.Key, mask(change.Local, reveal), mask(change.Remote, reveal)); err != nil {
			return err
		}
	}
	for _, change := range result.Removed {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %s=%s  (remote only; push --prune deletes it)\n", r.out.apply(red, "-"), change.Key, mask(change.Remote, reveal)); err != nil {
			return err
		}
	}
	for _, key := range result.Withheld {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %s  (remote value withheld; cannot compare)\n", r.out.apply(dim, "?"), key); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(r.streams.Out, "%d unchanged\n", result.Unchanged)
	return err
}

func maskDiff(result service.EnvDiffResult) service.EnvDiffResult {
	hide := func(changes []service.EnvChange) []service.EnvChange {
		out := make([]service.EnvChange, len(changes))
		for i, change := range changes {
			out[i] = service.EnvChange{Key: change.Key}
		}
		return out
	}
	result.Added, result.Changed, result.Removed = hide(result.Added), hide(result.Changed), hide(result.Removed)
	return result
}

func (r *Renderer) EnvPull(result service.EnvPullResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	_, err := fmt.Fprintf(r.streams.Out, "Wrote %d %s variable(s) to %s", len(result.Written), result.Scope, singleLine(result.File))
	if err != nil {
		return err
	}
	if len(result.Kept) > 0 {
		if _, err := fmt.Fprintf(r.streams.Out, "; kept %d local-only", len(result.Kept)); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(r.streams.Out)
	return err
}

func (r *Renderer) EnvPush(result service.EnvPushResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	plan := result.Plan
	if plan.Empty() {
		_, err := fmt.Fprintf(r.streams.Out, "Nothing to push; %s variables of %s match %s\n", plan.Scope, singleLine(plan.Target.Application), singleLine(plan.File))
		return err
	}
	_, err := fmt.Fprintf(r.streams.Out, "Pushed to %s variables of %s: %d created, %d updated, %d deleted\n",
		plan.Scope, singleLine(plan.Target.Application), len(plan.Create), len(plan.Update), len(plan.Delete))
	return err
}

func (r *Renderer) Domain(result service.DomainResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	if len(result.Domains) == 0 {
		_, err := fmt.Fprintf(r.streams.Out, "%s has no domain\n", singleLine(result.Target.Application))
		return err
	}
	note := ""
	if result.Generated {
		note = "  (generated by Coolify)"
	}
	if result.Services == nil {
		for _, domain := range result.Domains {
			if _, err := fmt.Fprintf(r.streams.Out, "%s%s\n", singleLine(domain), note); err != nil {
				return err
			}
		}
		return nil
	}
	// A Compose application's domains belong to its services: one line per
	// URL, the service first, in one aligned column.
	width := 0
	for _, domain := range result.Services {
		width = max(width, len(singleLine(domain.Service)))
	}
	for _, domain := range result.Services {
		if _, err := fmt.Fprintf(r.streams.Out, "%-*s  %s%s\n", width, singleLine(domain.Service), singleLine(domain.URL), note); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) DomainSet(result service.DomainSetResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	_, err := fmt.Fprintf(r.streams.Out, "Domains of %s: %s\n", singleLine(result.Plan.Target.Application), singleLine(domainList(result.Plan.Domains, result.Plan.Services)))
	return err
}

// domainList names domains as the user types them: SERVICE=URL for a
// Compose application, whose per-service entries are given, URLs
// otherwise.
func domainList(urls []string, services []service.ServiceDomain) string {
	if services == nil {
		return strings.Join(urls, ", ")
	}
	parts := make([]string, 0, len(services))
	for _, domain := range services {
		if domain.Service == "" {
			parts = append(parts, domain.URL)
			continue
		}
		parts = append(parts, domain.Service+"="+domain.URL)
	}
	return strings.Join(parts, ", ")
}

func (r *Renderer) Login(result service.LoginResult) error {
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	verb := "Logged in to"
	if result.Replaced {
		verb = "Updated"
	}
	suffix := ""
	if result.Default {
		suffix = ", now the default"
	}
	_, err := fmt.Fprintf(r.streams.Out, "%s %s (%s) as team %s on Coolify %s%s\nSaved to %s\n",
		verb, singleLine(result.Name), singleLine(result.URL), singleLine(result.Team), singleLine(result.Server), suffix, singleLine(result.Path))
	return err
}

// LoginAfterForm is Login for a run that asked through the form, whose
// checklist already shows the URL, the team, the server's version, and where
// the context was saved. On a terminal at normal verbosity it says only what
// that checklist does not: that the login stands, and whether it became the
// default. A piped stdout, and a verbose run, get Login's full result.
func (r *Renderer) LoginAfterForm(result service.LoginResult) error {
	if r.format == "json" || !r.namesOnly() {
		return r.Login(result)
	}
	verb := "Logged in to"
	if result.Replaced {
		verb = "Updated"
	}
	suffix := "."
	if result.Default {
		suffix = ", now the default."
	}
	_, err := fmt.Fprintf(r.streams.Out, "%s %s%s\n", verb, r.out.apply(bold, singleLine(result.Name)), suffix)
	return err
}

// Alias reports what coolship alias did. Notes such as the directory not
// being on PATH go to stderr first, as warnings.
func (r *Renderer) Alias(result alias.Result) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	name, path := singleLine(result.Name), singleLine(result.Path)
	var err error
	switch result.Status {
	case alias.StatusCreated:
		_, err = fmt.Fprintf(r.streams.Out, "Added %s: %s runs coolship\n", r.out.apply(bold, name), path)
	case alias.StatusExists:
		_, err = fmt.Fprintf(r.streams.Out, "%s already runs coolship: %s\n", r.out.apply(bold, name), path)
	case alias.StatusRemoved:
		_, err = fmt.Fprintf(r.streams.Out, "Removed %s: %s\n", r.out.apply(bold, name), path)
	case alias.StatusAbsent:
		_, err = fmt.Fprintf(r.streams.Out, "No alias %s at %s; nothing was removed\n", r.out.apply(bold, name), path)
	}
	return err
}

func (r *Renderer) Logout(result service.LogoutResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	_, err := fmt.Fprintf(r.streams.Out, "Removed %s from %s\n", singleLine(result.Name), singleLine(result.Path))
	return err
}
