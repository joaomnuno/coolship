// Package service implements project-local workflows independently of terminal presentation.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/resolver"
)

// Options identifies a local project and explicit invocation overrides.
type Options struct {
	CWD           string
	ConfigPath    string
	Context       string
	CoolifyConfig string
	Environment   string
}

type LinkOptions struct {
	Options
	Project         string
	Application     string
	ProjectUUID     string
	EnvironmentUUID string
	ApplicationUUID string
	Root            string
	Replace         bool
}

type DeployOptions struct {
	Options
	Force   bool
	NoWait  bool
	Timeout time.Duration
}

type LogsOptions struct {
	Options
	Lines  int
	Follow bool
}

// TargetInfo is the credential-free identity included in public command results.
type TargetInfo struct {
	Instance        string `json:"instance"`
	InstanceURL     string `json:"instance_url"`
	Project         string `json:"project"`
	ProjectUUID     string `json:"project_uuid"`
	Environment     string `json:"environment"`
	EnvironmentUUID string `json:"environment_uuid"`
	Application     string `json:"application"`
	ApplicationUUID string `json:"application_uuid"`
	Root            string `json:"root"`
}

type StatusResult struct {
	Target   TargetInfo `json:"target"`
	Status   string     `json:"status"`
	URL      string     `json:"url,omitempty"`
	Warnings []string   `json:"warnings,omitempty"`
}

type DeployResult struct {
	Target         TargetInfo `json:"target"`
	DeploymentUUID string     `json:"deployment_uuid"`
	Status         string     `json:"status"`
	Warnings       []string   `json:"warnings,omitempty"`
}

type Choice struct{ ID, Name, Detail string }
type Selector func(context.Context, string, []Choice) (string, error)
type Confirm func(context.Context, LinkPlan) (bool, error)

type LinkPlan struct {
	Path      string     `json:"path"`
	Target    TargetInfo `json:"target"`
	Replacing bool       `json:"replacing"`
}

type LinkResult struct {
	Path     string     `json:"path"`
	Target   TargetInfo `json:"target"`
	Warnings []string   `json:"warnings,omitempty"`
}

// Event carries requested logs or workflow progress. It never contains credentials.
type Event struct {
	Type           string `json:"type"`
	DeploymentUUID string `json:"deployment_uuid,omitempty"`
	Status         string `json:"status,omitempty"`
	Message        string `json:"message,omitempty"`
	Logs           string `json:"logs,omitempty"`
}

type Emitter func(Event) error

// Backend composes the small endpoint contracts used by a prepared session.
type Backend interface {
	resolver.Catalog
	Deploy(context.Context, string, bool) ([]models.DeploymentReceipt, error)
	GetDeployment(context.Context, string) (models.Deployment, error)
	Logs(context.Context, string, int) (models.LogSnapshot, error)
}

type Dependencies struct {
	ResolveCredentials func(auth.Options) (auth.Credentials, error)
	ListInstances      func(auth.Options) ([]auth.Instance, error)
	NewBackend         func(auth.Credentials) (Backend, error)
	CredentialURL      string
	CredentialToken    string
	PollInterval       time.Duration
}

var ErrInput = errors.New("invalid command input")
var ErrCancelled = errors.New("operation cancelled")

// InputError classifies actionable local configuration and argument failures.
type InputError struct{ Err error }

func (e *InputError) Error() string        { return e.Err.Error() }
func (e *InputError) Unwrap() error        { return e.Err }
func (e *InputError) Is(target error) bool { return target == ErrInput }

// DeploymentError retains recovery information even if observation fails.
type DeploymentError struct {
	DeploymentUUID string
	Err            error
}

func (e *DeploymentError) Error() string {
	return "deployment " + e.DeploymentUUID + ": " + e.Err.Error()
}
func (e *DeploymentError) Unwrap() error { return e.Err }
