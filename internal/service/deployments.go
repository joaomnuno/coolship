package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/joaomnuno/coolship/internal/models"
)

// cancelSearchWindow bounds how far back cancel looks for an in-progress
// deployment when none is named. Queued and running rows are the newest ones.
const cancelSearchWindow = 25

// Deployments lists the newest deployments of the linked application.
func (a *App) Deployments(ctx context.Context, options DeploymentsOptions) (DeploymentsResult, error) {
	if options.Limit <= 0 {
		return DeploymentsResult{}, input(errors.New("deployment count must be positive"))
	}
	s, err := a.prepare(ctx, options.Options)
	if err != nil {
		return DeploymentsResult{}, err
	}
	page, err := s.backend.ListDeployments(ctx, s.project.Application.UUID, options.Limit)
	if err != nil {
		return DeploymentsResult{}, fmt.Errorf("read deployment history: %w", err)
	}
	result := DeploymentsResult{Target: targetInfo(s.project), Total: page.Total, Deployments: make([]DeploymentSummary, 0, len(page.Deployments)), Warnings: s.warnings}
	for _, record := range page.Deployments {
		result.Deployments = append(result.Deployments, summarize(record))
	}
	return result, nil
}

// summarize keeps the fields a developer reads and classifies the row. The
// server's build log never reaches the record, so nothing is dropped here.
func summarize(record models.DeploymentRecord) DeploymentSummary {
	kind := "deploy"
	switch {
	case record.PullRequest > 0:
		kind = "preview"
	case record.Rollback:
		kind = "rollback"
	case record.RestartOnly:
		kind = "restart"
	}
	source := "manual"
	switch {
	case record.IsWebhook:
		source = "webhook"
	case record.IsAPI:
		source = "api"
	}
	return DeploymentSummary{UUID: record.UUID, Status: record.Status, Commit: record.Commit, CommitMessage: strings.TrimSpace(record.CommitMessage),
		Kind: kind, Source: source, PullRequest: record.PullRequest, ForceRebuild: record.ForceRebuild, Server: record.ServerName,
		CreatedAt: record.CreatedAt, FinishedAt: record.FinishedAt}
}

// lastDeployment reads the newest history row for status. History is an
// addition to the status, so most failures become a warning, not an error.
// A cancelled or timed-out read is propagated instead, so the executable
// boundary classifies it like every other interrupted command rather than
// reporting a successful status with a note.
func (a *App) lastDeployment(ctx context.Context, s session) (*DeploymentSummary, string, error) {
	page, err := s.backend.ListDeployments(ctx, s.project.Application.UUID, 1)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, "", err
		}
		return nil, "Deployment history could not be read: " + err.Error(), nil
	}
	if len(page.Deployments) == 0 {
		return nil, "", nil
	}
	last := summarize(page.Deployments[0])
	return &last, "", nil
}

// Cancel cancels one deployment of the linked application: the named one,
// or the single queued or in-progress one. The server only cancels those two
// states, so anything else is refused here before a request.
func (a *App) Cancel(ctx context.Context, options CancelOptions, confirm ConfirmCancel) (CancelResult, error) {
	s, err := a.prepare(ctx, options.Options)
	if err != nil {
		return CancelResult{}, err
	}
	result := CancelResult{Target: targetInfo(s.project), Warnings: s.warnings}
	var target DeploymentSummary
	if options.DeploymentUUID != "" {
		target, err = a.namedDeployment(ctx, s, options.DeploymentUUID)
	} else {
		target, err = a.activeDeployment(ctx, s)
	}
	if err != nil {
		return result, err
	}
	result.DeploymentUUID = target.UUID
	if !cancellable(target.Status) {
		return result, input(fmt.Errorf("deployment %s is %s; only queued or in-progress deployments can be cancelled", target.UUID, target.Status))
	}
	if !options.Yes {
		if confirm == nil {
			return result, input(errors.New("cancelling a deployment requires --yes when input is noninteractive"))
		}
		accepted, err := confirm(ctx, CancelPlan{Target: result.Target, Deployment: target})
		if err != nil {
			return result, err
		}
		if !accepted {
			return result, ErrCancelled
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	receipt, err := s.backend.CancelDeployment(ctx, target.UUID)
	if err != nil {
		return result, fmt.Errorf("cancel deployment %s: %w", target.UUID, err)
	}
	result.Status, result.Message = receipt.Status, receipt.Message
	if receipt.DeploymentUUID != "" && receipt.DeploymentUUID != target.UUID {
		return result, fmt.Errorf("server answered for deployment %s instead of %s; inspect Coolify", receipt.DeploymentUUID, target.UUID)
	}
	if result.Status == "" {
		result.Status = "cancelled-by-user"
		result.Warnings = append(result.Warnings, "The server did not report the deployment's status; it is assumed cancelled.")
	}
	return result, nil
}

func cancellable(status string) bool {
	return status == "queued" || status == "in_progress"
}

// namedDeployment reads one deployment and checks it belongs to the linked
// application, so a UUID pasted from elsewhere cannot cancel someone else's
// deployment through this binding.
func (a *App) namedDeployment(ctx context.Context, s session, uuid string) (DeploymentSummary, error) {
	if strings.ContainsAny(uuid, "/ \t\r\n") {
		return DeploymentSummary{}, input(fmt.Errorf("deployment UUID %q is not a valid identifier", uuid))
	}
	deployment, err := s.backend.GetDeployment(ctx, uuid)
	if err != nil {
		if code, ok := httpStatus(err); ok && code == 404 {
			return DeploymentSummary{}, input(fmt.Errorf("deployment %s was not found", uuid))
		}
		return DeploymentSummary{}, fmt.Errorf("read deployment %s: %w", uuid, err)
	}
	if deployment.UUID != uuid {
		return DeploymentSummary{}, fmt.Errorf("server returned deployment %s for %s; inspect Coolify", deployment.UUID, uuid)
	}
	if deployment.Application.UUID != s.project.Application.UUID {
		return DeploymentSummary{}, input(fmt.Errorf("deployment %s does not belong to application %s (%s)", uuid, s.project.Application.Name, s.project.Application.UUID))
	}
	return DeploymentSummary{UUID: deployment.UUID, Status: deployment.Status, Commit: deployment.Commit, Kind: "deploy"}, nil
}

// activeDeployment finds the one deployment that can be cancelled. None or
// several are input errors that say what to pass instead.
func (a *App) activeDeployment(ctx context.Context, s session) (DeploymentSummary, error) {
	page, err := s.backend.ListDeployments(ctx, s.project.Application.UUID, cancelSearchWindow)
	if err != nil {
		return DeploymentSummary{}, fmt.Errorf("read deployment history: %w", err)
	}
	var active []DeploymentSummary
	for _, record := range page.Deployments {
		if cancellable(record.Status) {
			active = append(active, summarize(record))
		}
	}
	switch len(active) {
	case 0:
		return DeploymentSummary{}, input(fmt.Errorf("no deployment of %s is queued or in progress; pass a deployment UUID to cancel an older one", s.project.Application.Name))
	case 1:
		return active[0], nil
	}
	names := make([]string, 0, len(active))
	for _, deployment := range active {
		names = append(names, deployment.UUID+" ("+deployment.Status+")")
	}
	return DeploymentSummary{}, input(fmt.Errorf("%d deployments of %s are queued or in progress: %s; pass the UUID of the one to cancel", len(active), s.project.Application.Name, strings.Join(names, ", ")))
}

// httpStatus reads the status of a typed HTTP failure without importing the adapter.
func httpStatus(err error) (int, bool) {
	var coded interface{ HTTPStatusCode() int }
	if errors.As(err, &coded) {
		return coded.HTTPStatusCode(), true
	}
	return 0, false
}
