// Package service implements project-local workflows independently of terminal presentation.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/config"
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
	Target        string // named target in a monorepo configuration
}

type LinkOptions struct {
	Options         // Target names the [apps.<name>] table to write; empty writes [project]
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
	Force       bool
	NoWait      bool
	Timeout     time.Duration
	PullRequest int // deploy the preview Coolify holds for this pull request
}

type LogsOptions struct {
	Options
	Lines  int
	Follow bool
}

// DevOptions runs a local command with the target's runtime variables.
// Command runs directly; when empty, the binding's dev setting runs through
// the shell.
type DevOptions struct {
	Options
	Preview bool
	Command []string
}

// ProcessSpec is what the service asks the process runner to execute. It
// carries only the injected variables; the runner layers them over the
// inherited environment.
type ProcessSpec struct {
	Dir   string
	Args  []string
	Shell string
	Env   []string
}

// ExitError carries a child process's nonzero status to the exit code.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("command exited with status %d", e.Code) }

type DomainResult struct {
	Target  TargetInfo `json:"target"`
	Domains []string   `json:"domains"`
	// Generated reports Coolify's automatic <uuid>.<wildcard> domain.
	Generated bool     `json:"generated"`
	Warnings  []string `json:"warnings,omitempty"`
}

type DomainSetOptions struct {
	Options
	Domains  []string
	Redirect string
	Force    bool
	Yes      bool
}

type DomainPlan struct {
	Target   TargetInfo `json:"target"`
	Current  []string   `json:"current"`
	Domains  []string   `json:"domains"`
	Redirect string     `json:"redirect,omitempty"`
}

type ConfirmDomain func(context.Context, DomainPlan) (bool, error)

type DomainSetResult struct {
	Plan     DomainPlan `json:"plan"`
	Warnings []string   `json:"warnings,omitempty"`
}

// LoginOptions registers an instance in the Coolify CLI configuration.
type LoginOptions struct {
	ConfigPath string // explicit --coolify-config; empty means the default path
	Name       string
	URL        string
	Token      string `json:"-"`
	Default    bool
}

type LoginResult struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Path     string `json:"path"`
	Default  bool   `json:"default"`
	Server   string `json:"server"`
	Team     string `json:"team"`
	Replaced bool   `json:"replaced"`
}

type LogoutOptions struct {
	ConfigPath string
	Name       string
}

type LogoutResult struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Warnings []string `json:"warnings,omitempty"`
}

type OpenOptions struct {
	Options
	Dashboard bool
}

// OpenResult names the URL a command should open. Launching a browser is a
// process concern, so the service only resolves the destination.
type OpenResult struct {
	Target   TargetInfo `json:"target"`
	Kind     string     `json:"kind"`
	URL      string     `json:"url"`
	Warnings []string   `json:"warnings,omitempty"`
}

type UnlinkOptions struct {
	Options
	Yes bool
}

type UnlinkPlan struct {
	Path    string         `json:"path"`
	Binding config.Binding `json:"binding"`
}

type ConfirmUnlink func(context.Context, UnlinkPlan) (bool, error)

type UnlinkResult struct {
	Path string `json:"path"`
}

// ConfigResult is the effective local configuration for one invocation. It is
// computed without network access and never contains a token.
type ConfigResult struct {
	ConfigPath       string            `json:"config_path"`
	ConfigRoot       string            `json:"config_root"`
	GitRoot          string            `json:"git_root,omitempty"`
	Target           string            `json:"target"`
	AppRoot          string            `json:"app_root"`
	Binding          config.Binding    `json:"binding"`
	CredentialSource string            `json:"credential_source"`
	CredentialPath   string            `json:"credential_path,omitempty"`
	Instance         string            `json:"instance,omitempty"`
	InstanceURL      string            `json:"instance_url,omitempty"`
	Overrides        map[string]string `json:"overrides,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
}

// Check is one doctor finding. Status is ok, warning, failed, or skipped.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type DoctorResult struct {
	Checks []Check `json:"checks"`
	Failed bool    `json:"failed"`
}

// TargetInfo is the credential-free identity included in public command results.
type TargetInfo struct {
	Target          string `json:"target"`
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
	PullRequest    int        `json:"pull_request,omitempty"`
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
	// Converting reports that the file changes between the single [project]
	// form and named [apps.<name>] targets, which drops the other form.
	Converting bool `json:"converting,omitempty"`
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
	Version(context.Context) (string, error)
	Team(context.Context) (models.Team, error)
	Deploy(context.Context, models.DeployRequest) ([]models.DeploymentReceipt, error)
	GetDeployment(context.Context, string) (models.Deployment, error)
	Logs(context.Context, string, int) (models.LogSnapshot, error)
	ListEnvironmentVariables(context.Context, string) ([]models.EnvironmentVariable, error)
	UpsertEnvironmentVariables(context.Context, string, []models.EnvironmentVariableInput) error
	DeleteEnvironmentVariable(context.Context, string, string) error
	UpdateApplicationDomains(context.Context, string, models.DomainUpdate) error
}

type Dependencies struct {
	ResolveCredentials func(auth.Options) (auth.Credentials, error)
	ListInstances      func(auth.Options) ([]auth.Instance, error)
	InspectCredentials func(auth.Options) auth.Report
	SaveCredentials    func(path string, instance auth.Stored, makeDefault bool) (string, error)
	RemoveCredentials  func(path, name string) (string, bool, error)
	NewBackend         func(auth.Credentials) (Backend, error)
	CredentialURL      string
	CredentialToken    string
	PollInterval       time.Duration
	RunProcess         func(context.Context, ProcessSpec) (int, error)
}

var ErrInput = errors.New("invalid command input")
var ErrCancelled = errors.New("operation cancelled")
var ErrChecksFailed = errors.New("doctor found problems that need attention")

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
