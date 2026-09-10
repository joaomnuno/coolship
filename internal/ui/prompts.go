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
	style   palette
}

func NewPrompter(streams Streams) *Prompter {
	streams = streams.Normalized()
	return &Prompter{streams: streams, input: bufio.NewReader(streams.In), style: streams.errPalette()}
}

// question styles the line that asks for input; answers and details stay plain.
func (p *Prompter) question(text string) string {
	return p.style.apply(bold, text)
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
	if _, err := fmt.Fprintln(p.streams.Err, p.question("Select "+singleLine(kind)+":")); err != nil {
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
		if _, err := fmt.Fprintf(p.streams.Err, "  %s %s\n", p.style.apply(cyan, strconv.Itoa(index+1)+"."), label); err != nil {
			return "", err
		}
	}
	for {
		if _, err := fmt.Fprint(p.streams.Err, p.question(fmt.Sprintf("Choice [1-%d, q to cancel]:", len(choices)))+" "); err != nil {
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
	note := "Existing comments and unrelated configuration will be replaced."
	if plan.Converting {
		note = "The file changes form; bindings in the other form are dropped. " + note
	}
	if _, err := fmt.Fprintf(p.streams.Err,
		"%s\nNew binding: %s / %s / %s on %s\n%s\n%s ",
		p.question("Replace configuration in "+singleLine(plan.Path)+"?"),
		singleLine(plan.Target.Project), singleLine(plan.Target.Environment),
		singleLine(plan.Target.Application), singleLine(plan.Target.Instance), note, p.question("Confirm [y/N]:")); err != nil {
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
	case "server":
		return "--server"
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
	if _, err := fmt.Fprintln(p.streams.Err, p.question("Delete "+singleLine(plan.Path)+"?")); err != nil {
		return false, err
	}
	if len(plan.Targets) == 0 {
		b := plan.Binding
		if _, err := fmt.Fprintf(p.streams.Err, "Current binding: %s / %s / %s\n", singleLine(b.Project), singleLine(b.Environment), singleLine(b.Application)); err != nil {
			return false, err
		}
	} else {
		if _, err := fmt.Fprintf(p.streams.Err, "Every target in it is removed (%d):\n", len(plan.Targets)); err != nil {
			return false, err
		}
		for _, target := range plan.Targets {
			b := target.Binding
			if _, err := fmt.Fprintf(p.streams.Err, "  %s: %s / %s / %s\n", singleLine(target.Name), singleLine(b.Project), singleLine(b.Environment), singleLine(b.Application)); err != nil {
				return false, err
			}
		}
	}
	if _, err := fmt.Fprint(p.streams.Err, "The remote application is not affected.\n"+p.question("Confirm [y/N]:")+" "); err != nil {
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
	if _, err := fmt.Fprintln(p.streams.Err, p.question(fmt.Sprintf("Push %s variables of %s from %s?", plan.Scope, singleLine(plan.Target.Application), singleLine(plan.File)))); err != nil {
		return false, err
	}
	// Fixed order: what the push does, then what it leaves alone and why.
	for _, group := range []struct {
		label   string
		changes []service.EnvChange
	}{{"create", plan.Create}, {"update", plan.Update}, {"delete", plan.Delete}} {
		for _, change := range group.changes {
			if _, err := fmt.Fprintf(p.streams.Err, "  %s %s\n", group.label, singleLine(change.Key)); err != nil {
				return false, err
			}
		}
	}
	for _, key := range plan.Skipped {
		if _, err := fmt.Fprintf(p.streams.Err, "  skip   %s (remote value withheld; --force overwrites it)\n", singleLine(key)); err != nil {
			return false, err
		}
	}
	for _, key := range plan.Untouched {
		if _, err := fmt.Fprintf(p.streams.Err, "  keep   %s (remote only; --prune deletes it)\n", singleLine(key)); err != nil {
			return false, err
		}
	}
	if _, err := fmt.Fprint(p.streams.Err, p.question("Confirm [y/N]:")+" "); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// ConfirmInit shows everything init is about to create and write.
func (p *Prompter) ConfirmInit(ctx context.Context, plan service.InitPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.streams.Interactive {
		return false, &service.InputError{Err: errors.New("creating an application requires --yes when input is noninteractive")}
	}
	project := singleLine(plan.Project)
	if plan.NewProject {
		project += " (new)"
	}
	buildPack := singleLine(plan.BuildPack) + ", port " + strconv.Itoa(plan.Port)
	if plan.Static {
		buildPack += ", static"
	}
	binding := singleLine(plan.Path)
	if plan.Target != "" && plan.Target != "default" {
		binding += " [apps." + singleLine(plan.Target) + "]"
	}
	rows := [][2]string{
		{"Repository", singleLine(plan.Repository) + " (branch " + singleLine(plan.Branch) + ")"},
		{"Build pack", buildPack},
		{"Project", project},
		{"Environment", singleLine(plan.Environment)},
		{"Server", singleLine(plan.Server)},
		{"Binding", binding},
	}
	if plan.Deploy {
		rows = append(rows, [2]string{"Then", "deploy and wait for it"})
	}
	if _, err := fmt.Fprintln(p.streams.Err, p.question("Create application "+singleLine(plan.Name)+" on "+singleLine(plan.Instance)+"?")); err != nil {
		return false, err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(p.streams.Err, "  %-12s %s\n", row[0]+":", row[1]); err != nil {
			return false, err
		}
	}
	if _, err := fmt.Fprint(p.streams.Err, p.question("Confirm [y/N]:")+" "); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// ConfirmStop names the application and its environment, since stopping
// production is the case that must not be answered by reflex.
func (p *Prompter) ConfirmStop(ctx context.Context, plan service.StopPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.streams.Interactive {
		return false, &service.InputError{Err: errors.New("stopping the application requires --yes when input is noninteractive")}
	}
	return p.confirmLifecycle(ctx, "Stop "+singleLine(plan.Target.Application)+" in "+singleLine(plan.Target.Environment)+"?", plan.Target, plan.Status,
		"Its containers are stopped and removed; deploy or start brings them back.")
}

// ConfirmRestart shows what a restart interrupts before it is queued.
func (p *Prompter) ConfirmRestart(ctx context.Context, plan service.RestartPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.streams.Interactive {
		return false, &service.InputError{Err: errors.New("restarting the application requires --yes when input is noninteractive")}
	}
	return p.confirmLifecycle(ctx, "Restart "+singleLine(plan.Target.Application)+" in "+singleLine(plan.Target.Environment)+"?", plan.Target, plan.Status,
		"Coolify queues a deployment that restarts the containers.")
}

func (p *Prompter) confirmLifecycle(ctx context.Context, question string, target service.TargetInfo, status, note string) (bool, error) {
	environment := singleLine(target.Environment)
	if environment == "production" {
		environment = p.style.apply(bold, environment)
	}
	if _, err := fmt.Fprintf(p.streams.Err, "%s\n  Application: %s (%s)\n  Environment: %s\n  Project:     %s\n  Status:      %s\n%s\n%s ",
		p.question(question), singleLine(target.Application), singleLine(target.ApplicationUUID), environment,
		singleLine(target.Project), singleLine(status), note, p.question("Confirm [y/N]:")); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// ConfirmCancel shows the deployment that is about to be cancelled.
func (p *Prompter) ConfirmCancel(ctx context.Context, plan service.CancelPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.streams.Interactive {
		return false, &service.InputError{Err: errors.New("cancelling a deployment requires --yes when input is noninteractive")}
	}
	d := plan.Deployment
	detail := singleLine(d.Status)
	if commit := shortCommit(d.Commit); commit != "" {
		detail += ", commit " + commit
	}
	if d.PullRequest > 0 {
		detail += ", pull request #" + strconv.Itoa(d.PullRequest)
	}
	if _, err := fmt.Fprintf(p.streams.Err, "%s\n  Deployment:  %s (%s)\n  Application: %s (%s)\n  Environment: %s\n%s ",
		p.question("Cancel deployment "+singleLine(d.UUID)+" of "+singleLine(plan.Target.Application)+"?"),
		singleLine(d.UUID), detail, singleLine(plan.Target.Application), singleLine(plan.Target.ApplicationUUID),
		singleLine(plan.Target.Environment), p.question("Confirm [y/N]:")); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// ConfirmDomain shows the replacement before the server is changed.
func (p *Prompter) ConfirmDomain(ctx context.Context, plan service.DomainPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.streams.Interactive {
		return false, &service.InputError{Err: errors.New("changing domains requires --yes when input is noninteractive")}
	}
	current := strings.Join(plan.Current, ", ")
	if current == "" {
		current = "(none)"
	}
	if _, err := fmt.Fprintf(p.streams.Err, "%s\n  from: %s\n  to:   %s\n%s ",
		p.question("Change domains of "+singleLine(plan.Target.Application)+"?"),
		singleLine(current), singleLine(strings.Join(plan.Domains, ", ")), p.question("Confirm [y/N]:")); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
