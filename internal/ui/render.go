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

	"github.com/joaomnuno/coolship/internal/service"
)

// Renderer keeps requested output separate from progress and diagnostics.
type Renderer struct {
	streams Streams
	format  string
	out     palette
	err     palette
}

func NewRenderer(streams Streams, format string) *Renderer {
	streams = streams.Normalized()
	return &Renderer{streams: streams, format: format, out: streams.outPalette(), err: streams.errPalette()}
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
	if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n", r.out.key("Status"), singleLine(result.Status)); err != nil {
		return err
	}
	if result.URL != "" {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n", r.out.key("URL"), singleLine(result.URL)); err != nil {
			return err
		}
	}
	if last := result.LastDeployment; last != nil {
		status := singleLine(last.Status)
		line := shortID(last.UUID) + " " + r.out.apply(deploymentStatus(status), status)
		if commit := shortCommit(last.Commit); commit != "" {
			line += " (" + commit + ")"
		}
		if when := localTime(last.CreatedAt); when != "" {
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
	rows := [][]string{{"UUID", "STATUS", "COMMIT", "TYPE", "CREATED", "DURATION"}}
	now := time.Now()
	for _, deployment := range result.Deployments {
		kind := singleLine(deployment.Kind)
		if deployment.PullRequest > 0 {
			kind = "preview #" + strconv.Itoa(deployment.PullRequest)
		}
		rows = append(rows, []string{shortID(deployment.UUID), singleLine(deployment.Status), shortCommit(deployment.Commit), kind,
			localTime(deployment.CreatedAt), duration(deployment.Status, deployment.CreatedAt, deployment.FinishedAt, now)})
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	for index, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			if i > 0 {
				line.WriteString("  ")
			}
			padding := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell))
			if index > 0 && i == 1 {
				cell = r.out.apply(deploymentStatus(cell), cell)
			}
			line.WriteString(cell)
			if i < len(row)-1 {
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
	_, err := fmt.Fprintf(r.streams.Out, "%s %s (%s)\n%s %s\n",
		r.out.key("Application"), singleLine(result.Target.Application), singleLine(result.Target.ApplicationUUID),
		r.out.key("Status"), singleLine(result.Status))
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
	_, err := fmt.Fprintf(r.streams.Out, "%s %s\n%s %s (%s)\n%s %s\n",
		r.out.key("Deployment"), singleLine(result.DeploymentUUID),
		r.out.key("Application"), singleLine(result.Target.Application), singleLine(result.Target.ApplicationUUID),
		r.out.key("Status"), r.out.apply(deploymentStatus(status), status))
	return err
}

func (r *Renderer) Link(result service.LinkResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
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
func (r *Renderer) Init(result service.InitResult) error {
	if err := r.warnings(result.Warnings); err != nil {
		return err
	}
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	plan := result.Plan
	if key := result.DeployKey; key != nil && result.Target.ApplicationUUID == "" {
		_, err := fmt.Fprintf(r.streams.Out, "Created deploy key %s (%s) on %s\n%s\n%s\n\nAdd it to %s as a read-only deploy key, then create the application with:\n  coolship init --source deploy-key --deploy-key %s\n",
			singleLine(key.Name), singleLine(key.UUID), singleLine(plan.Instance), r.out.key("Public key"), singleLine(key.PublicKey),
			singleLine(key.Repository), singleLine(key.Name))
		return err
	}
	buildPack := singleLine(plan.BuildPack)
	if plan.Port > 0 {
		buildPack += fmt.Sprintf(", port %d", plan.Port)
	}
	if plan.Static {
		buildPack += ", static"
	}
	if _, err := fmt.Fprintf(r.streams.Out, "Created application %s (%s) from %s at %s\n%s %s\n",
		singleLine(plan.Name), singleLine(result.Target.ApplicationUUID), singleLine(plan.Repository), singleLine(plan.Branch),
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
	if _, err := fmt.Fprintf(r.streams.Out, "%s %s\n", r.out.key("Deployment"), singleLine(result.DeploymentUUID)); err != nil {
		return err
	}
	if result.PullRequest > 0 {
		if _, err := fmt.Fprintf(r.streams.Out, "%s %d\n", r.out.key("Pull request"), result.PullRequest); err != nil {
			return err
		}
	}
	status := singleLine(result.Status)
	if _, err := fmt.Fprintf(r.streams.Out, "%s %s (%s)\n%s %s\n",
		r.out.key("Application"), singleLine(result.Target.Application), singleLine(result.Target.ApplicationUUID),
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
	if event.Type == "warning" {
		return r.warnings([]string{event.Message})
	}
	if event.Logs != "" {
		return writeLogs(r.streams.Err, event.Logs)
	}
	message := singleLine(event.Message)
	if event.Type == "application" {
		// Stop reports the server's receipt once, then each status it observes.
		if message == "" {
			message = "Application status: " + singleLine(event.Status)
		}
	} else if message == "" && event.Status != "" {
		status := singleLine(event.Status)
		message = "Deployment " + singleLine(event.DeploymentUUID) + ": " + r.err.apply(deploymentStatus(status), status)
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
		"%s %s (%s)\n%s %s\n%s %s\n%s %s\n",
		r.out.key("Application"), singleLine(target.Application), singleLine(target.ApplicationUUID),
		r.out.key("Environment"), singleLine(target.Environment),
		r.out.key("Project"), singleLine(target.Project),
		r.out.key("Context"), singleLine(target.Instance))
	return err
}

// Warn writes one warning to stderr in the style every result's warnings use.
func (r *Renderer) Warn(message string) error { return r.warnings([]string{message}) }

func (r *Renderer) warnings(warnings []string) error {
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(r.streams.Err, "%s %s\n", r.err.apply(yellow, "Warning:"), singleLine(warning)); err != nil {
			return err
		}
	}
	return nil
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
		if check.Detail != "" {
			line += ": " + singleLine(check.Detail)
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
	for _, domain := range result.Domains {
		note := ""
		if result.Generated {
			note = "  (generated by Coolify)"
		}
		if _, err := fmt.Fprintf(r.streams.Out, "%s%s\n", singleLine(domain), note); err != nil {
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
	_, err := fmt.Fprintf(r.streams.Out, "Domains of %s: %s\n", singleLine(result.Plan.Target.Application), singleLine(strings.Join(result.Plan.Domains, ", ")))
	return err
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
