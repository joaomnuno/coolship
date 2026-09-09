package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/joaomnuno/coolship/internal/service"
)

// Prompter shares one buffered input reader across all steps of a link workflow.
type Prompter struct {
	streams Streams
	input   *bufio.Reader
}

func NewPrompter(streams Streams) *Prompter {
	streams = streams.Normalized()
	return &Prompter{streams: streams, input: bufio.NewReader(streams.In)}
}

// Select returns a chosen ID; noninteractive execution never guesses a choice.
func (p *Prompter) Select(ctx context.Context, kind string, choices []service.Choice) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !p.streams.Interactive {
		return "", &service.InputError{Err: fmt.Errorf("select %s explicitly with %s when input is noninteractive", singleLine(kind), selectionFlags(kind))}
	}
	if len(choices) == 0 {
		return "", &service.InputError{Err: fmt.Errorf("no %s choices are available", singleLine(kind))}
	}
	if _, err := fmt.Fprintf(p.streams.Err, "Select %s:\n", singleLine(kind)); err != nil {
		return "", err
	}
	for index, choice := range choices {
		label := singleLine(choice.Name)
		if choice.ID != choice.Name {
			label += " (" + singleLine(choice.ID) + ")"
		}
		if choice.Detail != "" && choice.Detail != choice.ID {
			label += " — " + singleLine(choice.Detail)
		}
		if _, err := fmt.Fprintf(p.streams.Err, "  %d. %s\n", index+1, label); err != nil {
			return "", err
		}
	}
	for {
		if _, err := fmt.Fprintf(p.streams.Err, "Choice [1-%d, q to cancel]: ", len(choices)); err != nil {
			return "", err
		}
		answer, err := p.readLine(ctx)
		if err != nil {
			return "", err
		}
		if strings.EqualFold(answer, "q") || strings.EqualFold(answer, "quit") {
			return "", service.ErrCancelled
		}
		index, err := strconv.Atoi(answer)
		if err == nil && index >= 1 && index <= len(choices) {
			return choices[index-1].ID, nil
		}
		if _, err := fmt.Fprintln(p.streams.Err, "Enter a listed number, or q to cancel."); err != nil {
			return "", err
		}
	}
}

// Confirm requires an explicit yes before an existing configuration is replaced.
func (p *Prompter) Confirm(ctx context.Context, plan service.LinkPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.streams.Interactive {
		return false, &service.InputError{Err: errors.New("replacing existing configuration requires --replace when input is noninteractive")}
	}
	if _, err := fmt.Fprintf(p.streams.Err,
		"Replace configuration in %s?\nNew binding: %s / %s / %s on %s\nExisting comments and unrelated configuration will be replaced.\nConfirm [y/N]: ",
		singleLine(plan.Path), singleLine(plan.Target.Project), singleLine(plan.Target.Environment),
		singleLine(plan.Target.Application), singleLine(plan.Target.Instance)); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

func (p *Prompter) readLine(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type result struct {
		line string
		err  error
	}
	input := make(chan result, 1)
	// Generic io.Reader cannot be interrupted. The buffered send lets this read
	// finish independently if the command exits on cancellation first.
	go func() {
		line, err := p.input.ReadString('\n')
		input <- result{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case next := <-input:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if next.err != nil && !(errors.Is(next.err, io.EOF) && next.line != "") {
			if errors.Is(next.err, io.EOF) {
				return "", service.ErrCancelled
			}
			return "", fmt.Errorf("read selection: %w", next.err)
		}
		return strings.TrimSpace(next.line), nil
	}
}

func selectionFlags(kind string) string {
	switch strings.ToLower(kind) {
	case "context", "instance":
		return "--context"
	case "project":
		return "--project or --project-uuid"
	case "environment":
		return "--environment or --environment-uuid"
	case "application":
		return "--application or --application-uuid"
	default:
		return "the corresponding link flags"
	}
}

// ConfirmUnlink requires an explicit yes before the binding file is deleted.
func (p *Prompter) ConfirmUnlink(ctx context.Context, plan service.UnlinkPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.streams.Interactive {
		return false, &service.InputError{Err: errors.New("unlinking requires --yes when input is noninteractive")}
	}
	b := plan.Binding
	if _, err := fmt.Fprintf(p.streams.Err,
		"Delete %s?\nCurrent binding: %s / %s / %s\nThe remote application is not affected.\nConfirm [y/N]: ",
		singleLine(plan.Path), singleLine(b.Project), singleLine(b.Environment), singleLine(b.Application)); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// ConfirmPush shows the plan with keys only; values stay off the terminal.
func (p *Prompter) ConfirmPush(ctx context.Context, plan service.EnvPushPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.streams.Interactive {
		return false, &service.InputError{Err: errors.New("pushing variables requires --yes when input is noninteractive")}
	}
	if _, err := fmt.Fprintf(p.streams.Err, "Push %s variables of %s from %s?\n", plan.Scope, singleLine(plan.Target.Application), singleLine(plan.File)); err != nil {
		return false, err
	}
	for label, changes := range map[string][]service.EnvChange{"create": plan.Create, "update": plan.Update, "delete": plan.Delete} {
		for _, change := range changes {
			if _, err := fmt.Fprintf(p.streams.Err, "  %s %s\n", label, singleLine(change.Key)); err != nil {
				return false, err
			}
		}
	}
	if _, err := fmt.Fprint(p.streams.Err, "Confirm [y/N]: "); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
