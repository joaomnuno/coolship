package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/joaomnuno/coolship/internal/problem"
	"github.com/joaomnuno/coolship/internal/service"
)

// ExitCode maps successful, interrupted, input, and operational outcomes.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, context.Canceled), errors.Is(err, service.ErrCancelled):
		return 130
	case errors.Is(err, service.ErrInput):
		return 2
	}
	var exit *service.ExitError
	if errors.As(err, &exit) && exit.Code > 0 {
		return exit.Code
	}
	return 1
}

// PrintError is ReportError for human output.
func PrintError(streams Streams, err error) error { return ReportError(streams, "human", err) }

// ReportError renders the single diagnostic owned by the executable boundary.
// A catalogued failure prints "Error [code]: message", then its hint and
// documentation link, on stderr; anything else keeps its plain "Error:" line.
// An interrupt is reported as such, keeping any recovery detail wrapped
// around it. With --format json the same failure is also written to stdout
// as {"error": {"code", "message", "hint", "docs_url"}}, using a generic code
// when the catalog has no entry. A child process's status prints nothing in
// either format: the child has already said what it had to say.
func ReportError(streams Streams, format string, err error) error {
	if err == nil {
		return nil
	}
	streams = streams.Normalized()
	var exit *service.ExitError
	if errors.As(err, &exit) && !errors.Is(err, context.Canceled) {
		return nil
	}
	var reported *reportedError
	if errors.As(err, &reported) {
		return nil
	}
	report, catalogued := describeError(err)
	if writeErr := writeErrorText(streams, report, catalogued); writeErr != nil {
		return writeErr
	}
	var written *resultWrittenError
	if format != "json" || errors.As(err, &written) {
		return nil
	}
	return json.NewEncoder(streams.Out).Encode(errorObject{Error: errorBody{
		Code: string(report.Code), Message: report.Message, Hint: report.Hint, DocsURL: report.DocsURL}})
}

// Reported marks a failure whose diagnostic is already on screen, such as
// the one printed above an offer to fix it, so ReportError does not print it
// again. The failure keeps its exit code and its place in the error chain.
func Reported(err error) error {
	if err == nil {
		return nil
	}
	return &reportedError{err: err}
}

type reportedError struct{ err error }

func (e *reportedError) Error() string { return e.err.Error() }
func (e *reportedError) Unwrap() error { return e.err }

// ResultWritten marks a failure whose command already wrote its JSON result
// on stdout, such as doctor's checks or a failed deployment's result. That
// document already says what failed, so with --format json no error object
// follows it and stdout stays one JSON value. The text on stderr and the
// exit code are unchanged.
func ResultWritten(err error) error {
	if err == nil {
		return nil
	}
	return &resultWrittenError{err: err}
}

type resultWrittenError struct{ err error }

func (e *resultWrittenError) Error() string { return e.err.Error() }
func (e *resultWrittenError) Unwrap() error { return e.err }

type errorObject struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
	DocsURL string `json:"docs_url"`
}

// describeError classifies err through the catalog, or gives it a generic
// code, and reports whether the catalog knew it.
func describeError(err error) (problem.Problem, bool) {
	text := err.Error()
	if errors.Is(err, service.ErrCancelled) && !errors.Is(err, context.Canceled) {
		return problem.Problem{Code: problem.CodeCancelled, Message: "cancelled"}, false
	}
	// The catalog comes before the interrupt: an interrupt after a deployment
	// request was sent is an uncertain submission, not a safe stop.
	if found, ok := problem.Classify(err); ok {
		found.Message = strings.ReplaceAll(found.Message, context.Canceled.Error(), "interrupted")
		return found, true
	}
	if errors.Is(err, context.Canceled) {
		return problem.Problem{Code: problem.CodeInterrupted, Message: strings.ReplaceAll(text, context.Canceled.Error(), "interrupted")}, false
	}
	switch {
	case errors.Is(err, service.ErrChecksFailed):
		return problem.Problem{Code: problem.CodeChecksFailed, Message: text}, false
	case errors.Is(err, service.ErrInput):
		return problem.Problem{Code: problem.CodeInvalidInput, Message: text}, false
	}
	return problem.Problem{Code: problem.CodeUnclassified, Message: text}, false
}

func writeErrorText(streams Streams, report problem.Problem, catalogued bool) error {
	w, style := streams.Err, streams.errPalette()
	switch {
	case report.Code == problem.CodeCancelled:
		_, err := fmt.Fprintln(w, style.apply(dim, "Cancelled"))
		return err
	case report.Code == problem.CodeInterrupted && report.Message == "interrupted":
		_, err := fmt.Fprintln(w, style.apply(dim, "Interrupted"))
		return err
	case !catalogued:
		_, err := fmt.Fprintf(w, "%s %s\n", style.apply(redBold, "Error:"), singleLine(report.Message))
		return err
	}
	lines := []string{fmt.Sprintf("%s %s", style.apply(redBold, "Error ["+string(report.Code)+"]:"), singleLine(report.Message))}
	if report.Hint != "" {
		lines = append(lines, style.apply(dim, "Hint: "+report.Hint))
	}
	if report.DocsURL != "" {
		lines = append(lines, style.apply(dim, "Docs: "+report.DocsURL))
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}
