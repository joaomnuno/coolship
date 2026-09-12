// Package service implements project-local workflows independently of terminal presentation.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/gitinfo"
	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/resolver"
	"github.com/joaomnuno/coolship/internal/sshkey"
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

// InitOptions creates an application for the current repository and binds it.
// Repository and Branch are read from Git when empty; the build pack is
// detected from the application root when BuildOptions leaves it empty.
// Source chooses how Coolify clones: auto probes the remote anonymously and
// picks public when that works, otherwise asks which private source to use.
type InitOptions struct {
	Options
	BuildOptions
	Repository      string
	Branch          string
	Name            string // defaults to the repository name
	Project         string
	CreateProject   bool
	Server          string
	Source          string // auto, public, github-app, or deploy-key; empty means auto
	GitHubApp       string // exact name of the GitHub App to clone through
	DeployKey       string // exact name of an existing key to clone with
	CreateDeployKey string // name of a key to create and print for the repository
	Yes             bool
	Deploy          bool
	Timeout         time.Duration // deployment observation, when Deploy is set
}

// SourcePublic, SourceGitHubApp, and SourceDeployKey are the ways Coolify can
// clone a repository; SourceAuto lets init choose from what the remote allows.
const (
	SourceAuto      = "auto"
	SourcePublic    = "public"
	SourceGitHubApp = "github-app"
	SourceDeployKey = "deploy-key"
)

// InitPlan is everything init will create and write, shown before it does.
// Repository is the remote as Coolify will store it: https for a public
// clone or a GitHub App, the SSH form for a deploy key. Source names a
// private source; it is empty for a public clone, which keeps the JSON of a
// public plan as it was. NewDeployKey means only the key is created now: the
// application follows once the key is registered on the repository. Port is
// 0 for a Compose application, whose services publish their own ports; the
// build fields that do not apply to the pack are omitted.
type InitPlan struct {
	Path             string          `json:"path"`
	Target           string          `json:"target"`
	Root             string          `json:"root"`
	Repository       string          `json:"repository"`
	Branch           string          `json:"branch"`
	BuildPack        string          `json:"build_pack"`
	Port             int             `json:"port"`
	Static           bool            `json:"static,omitempty"`
	PublishDirectory string          `json:"publish_directory,omitempty"`
	Dockerfile       string          `json:"dockerfile,omitempty"`
	ComposeFile      string          `json:"compose_file,omitempty"`
	ComposeDomains   []ComposeDomain `json:"compose_domains,omitempty"`
	InstallCommand   string          `json:"install_command,omitempty"`
	BuildCommand     string          `json:"build_command,omitempty"`
	StartCommand     string          `json:"start_command,omitempty"`
	Name             string          `json:"name"`
	Instance         string          `json:"instance"`
	Project          string          `json:"project"`
	NewProject       bool            `json:"new_project,omitempty"`
	Environment      string          `json:"environment"`
	Server           string          `json:"server"`
	Deploy           bool            `json:"deploy,omitempty"`
	Source           string          `json:"source,omitempty"`
	GitHubApp        string          `json:"github_app,omitempty"`
	DeployKey        string          `json:"deploy_key,omitempty"`
	NewDeployKey     bool            `json:"new_deploy_key,omitempty"`
	// Warnings are what the confirmation must show before the answer. They
	// are reported again in InitResult.Warnings, so the plan's JSON omits
	// them.
	Warnings []string `json:"-"`
}

type ConfirmInit func(context.Context, InitPlan) (bool, error)

type InitResult struct {
	Plan   InitPlan   `json:"plan"`
	Target TargetInfo `json:"target"`
	// URL is the domain Coolify assigned at creation.
	URL string `json:"url,omitempty"`
	// DeployKey is the key init created. Its public half must be registered
	// on the repository before Coolify can clone; when it is set, no
	// application was created and Target is empty.
	DeployKey  *DeployKeyResult `json:"deploy_key,omitempty"`
	Deployment *DeployResult    `json:"deployment,omitempty"`
	Warnings   []string         `json:"warnings,omitempty"`
}

// DeployKeyResult describes a key init created in Coolify. PublicKey is the
// authorized_keys line to add to the repository; the private half stays in
// Coolify and is never part of a result.
type DeployKeyResult struct {
	Name       string `json:"name"`
	UUID       string `json:"uuid"`
	PublicKey  string `json:"public_key"`
	Repository string `json:"repository"`
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

// ExitError carries a nonzero status that is the whole answer, so the
// boundary prints nothing for it: a child process has already said what it
// had to say, and a comparison asked for a status reports through it alone.
// Err is the optional cause, kept for errors.Is.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("command exited with status %d", e.Code)
}
func (e *ExitError) Unwrap() error { return e.Err }

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

// UnlinkPlan is what unlink deletes. Binding is the single [project] form;
// Targets lists every [apps.<name>] table of the named form, all of which go
// with the file.
type UnlinkPlan struct {
	Path    string         `json:"path"`
	Binding config.Binding `json:"binding"`
	Targets []UnlinkTarget `json:"targets,omitempty"`
}

// UnlinkTarget is one named binding an unlink removes.
type UnlinkTarget struct {
	Name    string         `json:"name"`
	Binding config.Binding `json:"binding"`
}

type ConfirmUnlink func(context.Context, UnlinkPlan) (bool, error)

type UnlinkResult struct {
	Path string `json:"path"`
}

// ConfigResult is the effective local configuration for one invocation. It is
// computed without network access and never contains a token.
type ConfigResult struct {
	ConfigPath       string             `json:"config_path"`
	ConfigRoot       string             `json:"config_root"`
	GitRoot          string             `json:"git_root,omitempty"`
	Target           string             `json:"target"`
	AppRoot          string             `json:"app_root"`
	Binding          config.Binding     `json:"binding"`
	CredentialSource string             `json:"credential_source"`
	CredentialPath   string             `json:"credential_path,omitempty"`
	Instance         string             `json:"instance,omitempty"`
	InstanceURL      string             `json:"instance_url,omitempty"`
	Overrides        map[string]string  `json:"overrides,omitempty"`
	Preferences      *PreferencesReport `json:"preferences,omitempty"`
	Warnings         []string           `json:"warnings,omitempty"`
}

// PreferencesReport is what config shows of the developer's preferences
// file: where it is, whether it exists, and its keys. Error is set when the
// file exists but is ignored because it cannot be read or parsed.
type PreferencesReport struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
	preferences.Preferences
	Error string `json:"error,omitempty"`
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
	Target TargetInfo `json:"target"`
	Status string     `json:"status"`
	URL    string     `json:"url,omitempty"`
	// LastDeployment is the newest row of the deployment history, when the
	// history could be read and is not empty.
	LastDeployment *DeploymentSummary `json:"last_deployment,omitempty"`
	Warnings       []string           `json:"warnings,omitempty"`
}

type DeployResult struct {
	Target         TargetInfo `json:"target"`
	DeploymentUUID string     `json:"deployment_uuid"`
	PullRequest    int        `json:"pull_request,omitempty"`
	// Action names the server action that queued the deployment, start or
	// restart; a plain deploy leaves it empty.
	Action string `json:"action,omitempty"`
	Status string `json:"status"`
	// URL is where the deployment can be seen once it has an identity: the
	// application when the deployment finished and the application has a
	// domain (URLKind "application"), otherwise its page in Coolify
	// (URLKind "deployment"). Both are empty when nothing was queued.
	URL      string   `json:"url,omitempty"`
	URLKind  string   `json:"url_kind,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// StopOptions stops the application's containers. Timeout bounds the wait
// for the status to leave running.
type StopOptions struct {
	Options
	Yes     bool
	Timeout time.Duration
}

// StopPlan is what stop shows before asking: the application and the state
// it is in now.
type StopPlan struct {
	Target TargetInfo `json:"target"`
	Status string     `json:"status"`
}

type ConfirmStop func(context.Context, StopPlan) (bool, error)

// StopResult reports the status observed before the request and the last
// one observed after it. Message is the server's receipt.
type StopResult struct {
	Target   TargetInfo `json:"target"`
	Before   string     `json:"before"`
	Status   string     `json:"status"`
	Message  string     `json:"message,omitempty"`
	Warnings []string   `json:"warnings,omitempty"`
}

// StartOptions drives start and restart, which both queue a deployment the
// service observes like deploy.
type StartOptions struct {
	Options
	Force   bool // start only: rebuild without cache
	NoWait  bool
	Timeout time.Duration
	Yes     bool // restart only: skip confirmation
}

// RestartPlan is shown before a restart is queued.
type RestartPlan struct {
	Target TargetInfo `json:"target"`
	Status string     `json:"status"`
}

type ConfirmRestart func(context.Context, RestartPlan) (bool, error)

type DeploymentsOptions struct {
	Options
	Limit int
}

// DeploymentSummary is one deployment of the linked application, without its
// build log. Kind is deploy, restart, rollback, or preview; Source is api,
// webhook, or manual. Timestamps are the server's RFC 3339 strings;
// FinishedAt is empty while the deployment runs.
type DeploymentSummary struct {
	UUID          string `json:"deployment_uuid"`
	Status        string `json:"status"`
	Commit        string `json:"commit"`
	CommitMessage string `json:"commit_message,omitempty"`
	Kind          string `json:"kind"`
	Source        string `json:"source"`
	PullRequest   int    `json:"pull_request,omitempty"`
	ForceRebuild  bool   `json:"force_rebuild,omitempty"`
	Server        string `json:"server,omitempty"`
	CreatedAt     string `json:"created_at"`
	FinishedAt    string `json:"finished_at,omitempty"`
}

type DeploymentsResult struct {
	Target      TargetInfo          `json:"target"`
	Total       int                 `json:"total"`
	Deployments []DeploymentSummary `json:"deployments"`
	Warnings    []string            `json:"warnings,omitempty"`
}

// CancelOptions names the deployment to cancel; empty means the one in
// progress for the linked application, which must be exactly one.
type CancelOptions struct {
	Options
	DeploymentUUID string
	Yes            bool
}

type CancelPlan struct {
	Target     TargetInfo        `json:"target"`
	Deployment DeploymentSummary `json:"deployment"`
}

type ConfirmCancel func(context.Context, CancelPlan) (bool, error)

// CancelResult carries the server's answer: the status it set and its receipt.
type CancelResult struct {
	Target         TargetInfo `json:"target"`
	DeploymentUUID string     `json:"deployment_uuid"`
	Status         string     `json:"status"`
	Message        string     `json:"message,omitempty"`
	Warnings       []string   `json:"warnings,omitempty"`
}

// Choice is one candidate a selector offers. Detail tells candidates that
// share a name apart; Current marks the one the existing binding names, so a
// selector can start on it when a directory is linked again.
type Choice struct {
	ID, Name, Detail string
	Current          bool
}
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

// Event carries requested logs or workflow progress. It never contains
// credentials. Type names what happened: "warning" carries Message;
// "deployment" carries the deployment's UUID and its Status, and the first
// one of a deployment also carries the Target it belongs to; "build" carries
// build log lines in Logs; "stage" carries a Stage (one of DeploymentStages)
// and its Status (started, done, or failed); "logs" carries runtime logs;
// "application" carries an application status.
type Event struct {
	Type           string      `json:"type"`
	DeploymentUUID string      `json:"deployment_uuid,omitempty"`
	Status         string      `json:"status,omitempty"`
	Stage          string      `json:"stage,omitempty"`
	Message        string      `json:"message,omitempty"`
	Logs           string      `json:"logs,omitempty"`
	Target         *TargetInfo `json:"target,omitempty"`
}

type Emitter func(Event) error

// Backend composes the small endpoint contracts used by a prepared session.
type Backend interface {
	resolver.Catalog
	Version(context.Context) (string, error)
	Team(context.Context) (models.Team, error)
	Deploy(context.Context, models.DeployRequest) ([]models.DeploymentReceipt, error)
	GetDeployment(context.Context, string) (models.Deployment, error)
	ListDeployments(ctx context.Context, applicationUUID string, take int) (models.DeploymentPage, error)
	StopApplication(context.Context, string) (string, error)
	StartApplication(ctx context.Context, applicationUUID string, force bool) (models.ActionReceipt, error)
	RestartApplication(context.Context, string) (models.ActionReceipt, error)
	CancelDeployment(context.Context, string) (models.CancelReceipt, error)
	Logs(context.Context, string, int) (models.LogSnapshot, error)
	ListEnvironmentVariables(context.Context, string) ([]models.EnvironmentVariable, error)
	UpsertEnvironmentVariables(context.Context, string, []models.EnvironmentVariableInput) error
	DeleteEnvironmentVariable(context.Context, string, string) error
	UpdateApplicationDomains(context.Context, string, models.DomainUpdate) error
	ListServers(context.Context) ([]models.Server, error)
	CreateProject(ctx context.Context, name, description string) (models.Project, error)
	CreateApplication(context.Context, models.ApplicationSpec) (models.CreatedApplication, error)
	ListGitHubApps(context.Context) ([]models.GitHubApp, error)
	ListGitHubBranches(ctx context.Context, appID int, owner, repo string) ([]models.GitHubBranch, error)
	ListPrivateKeys(context.Context) ([]models.PrivateKey, error)
	CreatePrivateKey(ctx context.Context, name, description, privateKey string) (models.PrivateKey, error)
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
	// InspectRepository reads the remote and branch of the repository
	// containing a directory; init asks it only for what --repo and --branch
	// did not supply. Without it, both flags are required.
	InspectRepository func(context.Context, string) (gitinfo.Repository, error)
	// ProbeRemote lists the branches of a remote as an anonymous client sees
	// them, or fails when credentials would be needed; init uses it to tell a
	// public repository from a private one. Without it, --source is required.
	ProbeRemote func(context.Context, string) ([]string, error)
	// GenerateKey creates the pair a new deploy key is made of; the default
	// is an Ed25519 pair in OpenSSH format.
	GenerateKey func(comment string) (sshkey.Pair, error)
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

// TimeoutError reports that --timeout elapsed while a deployment was being
// observed. It unwraps to the deadline error so errors.Is still recognizes
// the timeout, but names the flag instead of the context.
type TimeoutError struct {
	Timeout time.Duration
	Err     error
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("--timeout %s elapsed before the deployment finished; it continues on the server", e.Timeout)
}
func (e *TimeoutError) Unwrap() error { return e.Err }

// restated is a lower-layer failure reworded for the command that hit it. The
// cause stays reachable through Unwrap for errors.Is and errors.As, but its
// own text is not repeated.
type restated struct {
	text string
	err  error
}

func (e *restated) Error() string { return e.text }
func (e *restated) Unwrap() error { return e.err }

func restate(cause error, format string, args ...any) error {
	return &restated{text: fmt.Sprintf(format, args...), err: cause}
}
