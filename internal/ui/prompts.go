package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joaomnuno/coolship/internal/service"
)

// Prompter shares one buffered input reader across all steps of a link workflow.
// When the streams carry a Block, the questions of init and link are asked
// inside its bar.
type Prompter struct {
	streams Streams
	input   *bufio.Reader
	style   palette
	block   *Block
}

func NewPrompter(streams Streams) *Prompter {
	streams = streams.Normalized()
	return &Prompter{streams: streams, input: bufio.NewReader(streams.In), style: streams.errPalette(), block: streams.Block}
}

// question styles the line that asks for input; answers and details stay plain.
func (p *Prompter) question(text string) string {
	return p.style.apply(bold, text)
}

// Select returns a chosen ID; noninteractive execution never guesses a choice.
// On a terminal it is an arrow-key picker (see pick); when input is
// interactive but not a terminal it prints a numbered list and reads a line.
// Both show names only, with a detail only where two names are the same.
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
	if in, errTerminal, ok := terminalInput(p.streams); ok {
		return p.pick(ctx, kind, choices, in, errTerminal)
	}
	if _, err := fmt.Fprintln(p.streams.Err, p.question("Select "+singleLine(kind)+":")); err != nil {
		return "", err
	}
	for index, label := range choiceLabels(choices) {
		if choices[index].Current {
			label += " (current)"
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
	if err := p.block.println(p.streams.Err, fmt.Sprintf("%s\nNew binding: %s / %s / %s on %s\n%s",
		p.question("Replace configuration in "+singleLine(plan.Path)+"?"),
		singleLine(plan.Target.Project), singleLine(plan.Target.Environment),
		singleLine(plan.Target.Application), singleLine(plan.Target.Instance), note)); err != nil {
		return false, err
	}
	if _, err := fmt.Fprint(p.streams.Err, p.block.prompt(p.question("Confirm [y/N]:")+" ")); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// YesNo asks a question whose Enter answer is the default, shown as [Y/n] or
// [y/N]. An answer that is neither yes nor no asks again. Noninteractive
// input answers no without asking; end of input cancels.
func (p *Prompter) YesNo(ctx context.Context, question string, defaultYes bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.streams.Interactive {
		return false, nil
	}
	choices := "[y/N]"
	if defaultYes {
		choices = "[Y/n]"
	}
	for {
		if _, err := fmt.Fprint(p.streams.Err, p.block.prompt(p.question(singleLine(question)+" "+choices)+" ")); err != nil {
			return false, err
		}
		answer, err := p.readLine(ctx)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		if err := p.block.println(p.streams.Err, "Answer y or n."); err != nil {
			return false, err
		}
	}
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
	case "source":
		return "--source"
	case "github app":
		return "--github-app"
	case "deploy key":
		return "--deploy-key or --create-deploy-key"
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

// ConfirmInit shows everything init is about to create and write, with any
// warnings above the question. With a new deploy key, only the key is created
// at this point, and the plan says so.
//
// Inside a Block the plan is a table without colons: the repository by host
// and path with the word private when the anonymous probe failed, in place
// of that warning; the project and environment on one line; and the binding
// relative to the working directory. Anywhere else it is the text it was.
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
	buildPack := singleLine(plan.BuildPack)
	if plan.Port > 0 {
		buildPack += ", port " + strconv.Itoa(plan.Port)
	}
	if plan.Static {
		buildPack += ", static"
	}
	binding := singleLine(plan.Path)
	if plan.Target != "" && plan.Target != "default" {
		binding += " [apps." + singleLine(plan.Target) + "]"
	}
	source := "public (cloned without credentials)"
	switch plan.Source {
	case service.SourceGitHubApp:
		source = "GitHub App " + singleLine(plan.GitHubApp)
	case service.SourceDeployKey:
		source = "deploy key " + singleLine(plan.DeployKey)
		if plan.NewDeployKey {
			source += " (new; the application is created once the key is registered on the repository)"
		}
	}
	rows := [][2]string{
		{"Repository", singleLine(plan.Repository) + " (branch " + singleLine(plan.Branch) + ")"},
		{"Source", source},
		{"Build pack", buildPack},
	}
	for _, detail := range buildDetails(plan) {
		rows = append(rows, [2]string{"  " + detail[0], detail[1]})
	}
	warnings := plan.Warnings
	if p.block.Active() {
		repository := repositoryLabel(singleLine(plan.Repository)) + " (" + singleLine(plan.Branch) + ")"
		warnings = nil
		for _, warning := range plan.Warnings {
			if plan.Private == "" || warning != plan.Private {
				warnings = append(warnings, warning)
			}
		}
		if plan.Private != "" {
			repository += ", private"
		}
		binding = relativeBinding(plan.Directory, plan.Path) + strings.TrimPrefix(binding, singleLine(plan.Path))
		details := rows[3:]
		rows = append([][2]string{{"Repository", repository}, {"Source", source}, {"Build pack", buildPack}}, details...)
		rows = append(rows, [][2]string{
			{"Project", project + " / " + singleLine(plan.Environment)},
			{"Server", singleLine(plan.Server)},
			{"Binding", binding},
		}...)
	} else {
		rows = append(rows, [][2]string{
			{"Project", project},
			{"Environment", singleLine(plan.Environment)},
			{"Server", singleLine(plan.Server)},
			{"Binding", binding},
		}...)
	}
	if plan.Deploy && !plan.NewDeployKey {
		rows = append(rows, [2]string{"Then", "deploy and wait for it"})
	}
	question := "Create application " + singleLine(plan.Name) + " on " + singleLine(plan.Instance) + "?"
	if plan.NewDeployKey {
		question = "Create deploy key " + singleLine(plan.DeployKey) + " on " + singleLine(plan.Instance) + " for " + singleLine(plan.Repository) + "?"
		rows = rows[:2]
	}
	// The warnings come first: they are the reason to answer no, so they
	// must be read before the question rather than after the creation.
	for _, warning := range warnings {
		if err := p.block.println(p.streams.Err, p.style.apply(yellow, "Warning:")+" "+singleLine(warning)); err != nil {
			return false, err
		}
	}
	if err := p.block.println(p.streams.Err, p.question(question)); err != nil {
		return false, err
	}
	column := 0
	for _, row := range rows {
		column = max(column, len([]rune(row[0])))
	}
	for _, row := range rows {
		lines := []string{fmt.Sprintf("  %-12s %s", row[0]+":", row[1])}
		if p.block.Active() {
			lines = p.block.hanging("  "+p.style.apply(dim, row[0])+pad(row[0], column+3), row[1])
		}
		if _, err := fmt.Fprintln(p.streams.Err, strings.Join(lines, "\n")); err != nil {
			return false, err
		}
	}
	if _, err := fmt.Fprint(p.streams.Err, p.block.prompt(p.question("Confirm [y/N]:")+" ")); err != nil {
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
	// Confirmations follow the stderr rule of the progress lines.
	namesOnly := p.streams.ErrTerminal && p.streams.level() == VerbosityNormal
	if _, err := fmt.Fprintf(p.streams.Err, "%s\n  Application: %s\n  Environment: %s\n  Project:     %s\n  Status:      %s\n%s\n%s ",
		p.question(question), named(target.Application, target.ApplicationUUID, namesOnly), environment,
		singleLine(target.Project), applicationState(p.style, p.streams.ErrTerminal, status), note, p.question("Confirm [y/N]:")); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// ConfirmDelete shows everything the deletion takes with it. Deletion is the
// one action here that nothing brings back, so the plan names the application's
// status and the URLs it is serving now: a developer who is about to delete the
// wrong application usually recognizes it by its domain.
func (p *Prompter) ConfirmDelete(ctx context.Context, plan service.DeletePlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.streams.Interactive {
		return false, &service.InputError{Err: errors.New("deleting the application requires --yes when input is noninteractive")}
	}
	environment := singleLine(plan.Target.Environment)
	if environment == "production" {
		environment = p.style.apply(bold, environment)
	}
	namesOnly := p.streams.ErrTerminal && p.streams.level() == VerbosityNormal
	if _, err := fmt.Fprintf(p.streams.Err, "%s\n  Application: %s\n  Environment: %s\n  Project:     %s\n  Status:      %s\n",
		p.question("Delete "+singleLine(plan.Target.Application)+" in "+singleLine(plan.Target.Environment)+"?"),
		named(plan.Target.Application, plan.Target.ApplicationUUID, namesOnly), environment,
		singleLine(plan.Target.Project), applicationState(p.style, p.streams.ErrTerminal, plan.Status)); err != nil {
		return false, err
	}
	for i, url := range plan.URLs {
		label := "URL:"
		if i > 0 {
			label = "    "
		}
		if _, err := fmt.Fprintf(p.streams.Err, "  %-12s %s\n", label, singleLine(url)); err != nil {
			return false, err
		}
	}
	volumes := "deleted with the application"
	if plan.KeepVolumes {
		volumes = "kept on the server"
	}
	if _, err := fmt.Fprintf(p.streams.Err, "  %-12s %s\n", "Volumes:", volumes); err != nil {
		return false, err
	}
	if plan.ConfigPath != "" {
		if _, err := fmt.Fprintf(p.streams.Err, "  %-12s %s is deleted too\n", "Binding:", singleLine(plan.ConfigPath)); err != nil {
			return false, err
		}
	}
	if _, err := fmt.Fprintf(p.streams.Err, "This cannot be undone.\n%s ", p.question("Confirm [y/N]:")); err != nil {
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
	namesOnly := p.streams.ErrTerminal && p.streams.level() == VerbosityNormal
	id := deploymentLabel(d.UUID, namesOnly)
	if _, err := fmt.Fprintf(p.streams.Err, "%s\n  Deployment:  %s (%s)\n  Application: %s\n  Environment: %s\n%s ",
		p.question("Cancel deployment "+id+" of "+singleLine(plan.Target.Application)+"?"),
		id, detail, named(plan.Target.Application, plan.Target.ApplicationUUID, namesOnly),
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
	current := domainList(plan.Current, plan.CurrentServices)
	if current == "" {
		current = "(none)"
	}
	if _, err := fmt.Fprintf(p.streams.Err, "%s\n  from: %s\n  to:   %s\n%s ",
		p.question("Change domains of "+singleLine(plan.Target.Application)+"?"),
		singleLine(current), singleLine(domainList(plan.Domains, plan.Services)), p.question("Confirm [y/N]:")); err != nil {
		return false, err
	}
	answer, err := p.readLine(ctx)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// repositoryLabel is a remote as a person names it: the host and the path,
// without the scheme, the user, or the .git a clone URL carries.
func repositoryLabel(remote string) string {
	label := remote
	if _, rest, ok := strings.Cut(label, "://"); ok {
		label = rest
	} else if host, path, ok := strings.Cut(label, ":"); ok {
		label = host + "/" + path
	}
	host, path, _ := strings.Cut(label, "/")
	if index := strings.LastIndex(host, "@"); index >= 0 {
		host = host[index+1:]
	}
	if host == "" || path == "" {
		return remote
	}
	return host + "/" + strings.TrimSuffix(strings.TrimRight(path, "/"), ".git")
}

// relativeBinding names the configuration file from the working directory,
// as ./coolship.toml or ./sub/coolship.toml, when it lies inside it, and by
// its full path otherwise.
func relativeBinding(directory, path string) string {
	if directory != "" {
		if relative, err := filepath.Rel(directory, path); err == nil && filepath.IsLocal(relative) {
			return singleLine("./" + filepath.ToSlash(relative))
		}
	}
	return singleLine(path)
}

// buildDetails lists the build settings that refine the pack, in the order
// Coolify's form shows them, so the plan and the result name them the same
// way. Every value is single-line already except the commands, which are
// the user's own text.
func buildDetails(plan service.InitPlan) [][2]string {
	var rows [][2]string
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, [2]string{label, singleLine(value)})
		}
	}
	add("Dockerfile", plan.Dockerfile)
	add("Compose file", plan.ComposeFile)
	for _, domain := range plan.ComposeDomains {
		add("Service "+singleLine(domain.Service), domain.Domain)
	}
	add("Publish dir", plan.PublishDirectory)
	add("Install", plan.InstallCommand)
	add("Build", plan.BuildCommand)
	add("Start", plan.StartCommand)
	return rows
}
