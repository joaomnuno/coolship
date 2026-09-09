package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/joaomnuno/coolship/internal/service"
)

// Renderer keeps requested output separate from progress and diagnostics.
type Renderer struct {
	streams Streams
	format  string
}

func NewRenderer(streams Streams, format string) *Renderer {
	return &Renderer{streams: streams.Normalized(), format: format}
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
	if _, err := fmt.Fprintf(r.streams.Out, "Status: %s\n", singleLine(result.Status)); err != nil {
		return err
	}
	if result.URL != "" {
		_, err := fmt.Fprintf(r.streams.Out, "URL: %s\n", singleLine(result.URL))
		return err
	}
	return nil
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

// Deploy renders the final result only. The same warnings already reached
// stderr as events before submission, so repeating them here would duplicate
// every warning in both formats.
func (r *Renderer) Deploy(result service.DeployResult) error {
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	_, err := fmt.Fprintf(r.streams.Out, "Deployment: %s\nApplication: %s (%s)\nStatus: %s\n",
		singleLine(result.DeploymentUUID), singleLine(result.Target.Application),
		singleLine(result.Target.ApplicationUUID), singleLine(result.Status))
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
	message := event.Message
	if message == "" && event.Status != "" {
		message = "Deployment " + event.DeploymentUUID + ": " + event.Status
	}
	if message == "" {
		return nil
	}
	_, err := fmt.Fprintln(r.streams.Err, singleLine(message))
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
	_, err := fmt.Fprintf(r.streams.Out,
		"Application: %s (%s)\nEnvironment: %s\nProject: %s\nContext: %s\n",
		singleLine(target.Application), singleLine(target.ApplicationUUID),
		singleLine(target.Environment), singleLine(target.Project), singleLine(target.Instance))
	return err
}

func (r *Renderer) warnings(warnings []string) error {
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(r.streams.Err, "Warning: %s\n", singleLine(warning)); err != nil {
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
	}
	for key, value := range result.Overrides {
		rows = append(rows, [2]string{"Override " + key, value})
	}
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		if _, err := fmt.Fprintf(r.streams.Out, "%-17s %s\n", row[0]+":", singleLine(row[1])); err != nil {
			return err
		}
	}
	return nil
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
// the same in every terminal and in CI logs.
func (r *Renderer) Doctor(result service.DoctorResult) error {
	if r.format == "json" {
		return json.NewEncoder(r.streams.Out).Encode(result)
	}
	markers := map[string]string{"ok": "[ok]  ", "warning": "[warn]", "failed": "[FAIL]", "skipped": "[skip]"}
	for _, check := range result.Checks {
		marker, known := markers[check.Status]
		if !known {
			marker = "[" + check.Status + "]"
		}
		line := marker + " " + check.Name
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
		if _, err := fmt.Fprintf(r.streams.Out, "+ %s=%s  (local only; push creates it)\n", change.Key, mask(change.Local, reveal)); err != nil {
			return err
		}
	}
	for _, change := range result.Changed {
		if _, err := fmt.Fprintf(r.streams.Out, "~ %s: local %s, remote %s\n", change.Key, mask(change.Local, reveal), mask(change.Remote, reveal)); err != nil {
			return err
		}
	}
	for _, change := range result.Removed {
		if _, err := fmt.Fprintf(r.streams.Out, "- %s=%s  (remote only; push --prune deletes it)\n", change.Key, mask(change.Remote, reveal)); err != nil {
			return err
		}
	}
	for _, key := range result.Withheld {
		if _, err := fmt.Fprintf(r.streams.Out, "? %s  (remote value withheld; cannot compare)\n", key); err != nil {
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
