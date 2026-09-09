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
