// Package models defines the small resource vocabulary shared by Coolship workflows.
package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type Project struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	// Environments is filled only by GET /projects/{uuid}, which embeds the
	// project's environments; the list endpoint leaves it nil.
	Environments []Environment `json:"environments,omitempty"`
}

type Environment struct {
	UUID         string        `json:"uuid"`
	Name         string        `json:"name"`
	Applications []Application `json:"applications,omitempty"`
}

type Application struct {
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// FQDN is Coolify's comma-separated domain list. A Docker Compose
	// application has none: its domains belong to its services and arrive
	// in ComposeDomains.
	FQDN string `json:"fqdn,omitempty"`
	// BuildPack is how Coolify builds the application; BuildPackCompose
	// marks a Docker Compose application.
	BuildPack string `json:"build_pack,omitempty"`
	// ComposeDomains are a Compose application's domains, one entry per
	// service, in the order Coolify stores them.
	ComposeDomains ComposeDomains `json:"docker_compose_domains,omitempty"`
}

// BuildPackCompose is the build pack of a Docker Compose application.
const BuildPackCompose = "dockercompose"

// IsCompose reports whether the application runs a Compose file, so its
// domains are per service. A listing that leaves out the build pack still
// tells by the per-service domains it carries, which no other kind has.
func (a Application) IsCompose() bool {
	return a.BuildPack == BuildPackCompose || (a.FQDN == "" && len(a.ComposeDomains) > 0)
}

// ComposeDomains is the per-service domain map of a Compose application as
// GET /applications/{uuid} sends it: a JSON object keyed by service name,
// encoded inside a JSON string, in the order the services were given. It
// decodes from that, from the same object sent bare, from an older shape
// whose values are the domain strings themselves, and from null, an empty
// string, or an empty array, which all mean no domains.
type ComposeDomains []ComposeDomain

func (d *ComposeDomains) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*d = nil
		return nil
	}
	if data[0] == '"' {
		var document string
		if err := json.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("docker_compose_domains: %w", err)
		}
		if strings.TrimSpace(document) == "" {
			*d = nil
			return nil
		}
		data = []byte(document)
	}
	parsed, err := parseComposeDomains(data)
	if err != nil {
		return fmt.Errorf("docker_compose_domains: %w", err)
	}
	*d = parsed
	return nil
}

// parseComposeDomains decodes the map itself. The object is read token by
// token so the services keep the order the document lists them in.
func parseComposeDomains(data []byte) (ComposeDomains, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('['):
		// Coolify's own default for a missing map, "[]", and the array the
		// update request takes.
		var entries []ComposeDomain
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, err
		}
		if len(entries) == 0 {
			return nil, nil
		}
		return ComposeDomains(entries), nil
	case json.Delim('{'):
	default:
		return nil, fmt.Errorf("expected an object keyed by service, got %v", token)
	}
	var result ComposeDomains
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, _ := key.(string)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		entry := ComposeDomain{Name: name}
		value = bytes.TrimSpace(value)
		switch {
		case len(value) == 0 || bytes.Equal(value, []byte("null")):
		case value[0] == '"':
			if err := json.Unmarshal(value, &entry.Domain); err != nil {
				return nil, err
			}
		case value[0] == '{':
			var fields struct {
				Domain   *string `json:"domain"`
				Redirect *string `json:"redirect"`
			}
			if err := json.Unmarshal(value, &fields); err != nil {
				return nil, fmt.Errorf("service %q: %w", name, err)
			}
			if fields.Domain != nil {
				entry.Domain = *fields.Domain
			}
			if fields.Redirect != nil {
				entry.Redirect = *fields.Redirect
			}
		default:
			return nil, fmt.Errorf("service %q has a domain that is neither a string nor an object", name)
		}
		result = append(result, entry)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return result, nil
}

type Deployment struct {
	UUID   string `json:"deployment_uuid"`
	Status string `json:"status"`
	Commit string `json:"commit,omitempty"`
	// Logs is the build log document as the server sends it: a JSON array
	// encoded inside a JSON string. It is nil when the token cannot read
	// sensitive data. Decode it with ParseDeploymentLogs.
	Logs *string `json:"logs,omitempty"`
	// Application is the owning application as GET /deployments/{uuid}
	// embeds it; the history list does not carry it.
	Application DeploymentOwner `json:"application"`
}

// DeploymentOwner is the part of an embedded application document a
// deployment consumer needs: its identity.
type DeploymentOwner struct {
	UUID string `json:"uuid"`
}

// DeploymentRecord is one row of an application's deployment history as
// Coolify's queue stores it, without the build log: the list endpoint sends
// logs to tokens that may read sensitive data, and nothing here keeps them.
// Commit is the full sha once the job resolved it, or HEAD while queued.
// FinishedAt is empty until the deployment ends. PullRequest is zero for the
// configured branch.
type DeploymentRecord struct {
	UUID          string `json:"deployment_uuid"`
	Status        string `json:"status"`
	Commit        string `json:"commit"`
	CommitMessage string `json:"commit_message"`
	PullRequest   int    `json:"pull_request_id"`
	ForceRebuild  bool   `json:"force_rebuild"`
	RestartOnly   bool   `json:"restart_only"`
	Rollback      bool   `json:"rollback"`
	IsWebhook     bool   `json:"is_webhook"`
	IsAPI         bool   `json:"is_api"`
	ServerName    string `json:"server_name"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	FinishedAt    string `json:"finished_at"`
}

// DeploymentPage is one page of an application's history, newest first, with
// the total the server counted.
type DeploymentPage struct {
	Total       int
	Deployments []DeploymentRecord
}

// ActionReceipt is the answer to an application start or restart. Coolify
// queues a deployment for both and names it; a message alone means it
// declined, e.g. because one is already queued for the same commit.
type ActionReceipt struct {
	Message        string `json:"message"`
	DeploymentUUID string `json:"deployment_uuid"`
}

// CancelReceipt is the answer to a successful deployment cancellation.
type CancelReceipt struct {
	Message        string `json:"message"`
	DeploymentUUID string `json:"deployment_uuid"`
	Status         string `json:"status"`
}

// DeploymentLogEntry is one line of a deployment's build log. Hidden entries
// carry the commands and internal output the server does not show in its UI.
type DeploymentLogEntry struct {
	Command   *string `json:"command"`
	Output    string  `json:"output"`
	Type      string  `json:"type"`
	Hidden    bool    `json:"hidden"`
	Timestamp string  `json:"timestamp"`
}

// ParseDeploymentLogs decodes the build log document. An empty document is a
// deployment that has not produced output yet, not an error.
func ParseDeploymentLogs(document string) ([]DeploymentLogEntry, error) {
	if strings.TrimSpace(document) == "" {
		return nil, nil
	}
	var entries []DeploymentLogEntry
	if err := json.Unmarshal([]byte(document), &entries); err != nil {
		return nil, fmt.Errorf("deployment logs are not a JSON array: %w", err)
	}
	return entries, nil
}

// DeployRequest names exactly one deployment. PullRequest selects the preview
// Coolify already holds for that pull request; zero means the configured branch.
type DeployRequest struct {
	ApplicationUUID string
	Force           bool
	PullRequest     int
}

type DeploymentReceipt struct {
	ResourceUUID   string `json:"resource_uuid"`
	DeploymentUUID string `json:"deployment_uuid"`
	Message        string `json:"message"`
}

type LogSnapshot struct {
	Logs string `json:"logs"`
}

// EnvironmentVariable is one variable in one scope. Value and RealValue are nil
// when the server withholds them (shown-once secrets), which is different from
// an empty value.
type EnvironmentVariable struct {
	UUID        string  `json:"uuid"`
	Key         string  `json:"key"`
	Value       *string `json:"value"`
	RealValue   *string `json:"real_value"`
	IsPreview   bool    `json:"is_preview"`
	IsBuildTime bool    `json:"is_buildtime"`
	IsRuntime   bool    `json:"is_runtime"`
	IsLiteral   bool    `json:"is_literal"`
	IsMultiline bool    `json:"is_multiline"`
	IsShared    bool    `json:"is_shared"`
	IsShownOnce bool    `json:"is_shown_once"`
	Comment     *string `json:"comment"`
}

// EnvironmentVariableInput is one create-or-update item. Coolify 4.3.18 keeps
// is_runtime, is_buildtime, and comment when they are absent, but resets
// is_literal, is_multiline, and is_shown_once to false, so an update must send
// the values it wants preserved.
type EnvironmentVariableInput struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	IsPreview   bool   `json:"is_preview"`
	IsLiteral   *bool  `json:"is_literal,omitempty"`
	IsMultiline *bool  `json:"is_multiline,omitempty"`
	IsShownOnce *bool  `json:"is_shown_once,omitempty"`
}

// DomainUpdate replaces an application's domains. Redirect is "www",
// "non-www", or "both"; Force bypasses the server's in-use check. Services,
// when set, replaces a Compose application's per-service map instead of
// Domains, each entry carrying its own redirect; Redirect is then unused.
type DomainUpdate struct {
	Domains  []string
	Redirect string
	Force    bool
	Services []ComposeDomain
}

// Server is a host Coolify can deploy to. IsUsable is the server's own
// readiness flag; IsReachable is its last connectivity check.
type Server struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	IP          string `json:"ip"`
	IsReachable bool   `json:"is_reachable"`
	IsUsable    bool   `json:"is_usable"`
}

// ApplicationSpec creates one application from a Git repository. The field
// names follow the server's request contract for the POST /applications/*
// creation endpoints; PortsExposes is a comma-separated port list. Source
// selects the endpoint and is not part of the body: empty or "public" clones
// anonymously, "github-app" clones through the GitHub App named by
// GitHubAppUUID, and "deploy-key" clones over SSH with the key named by
// PrivateKeyUUID, in which case GitRepository must be an SSH remote.
type ApplicationSpec struct {
	ProjectUUID     string `json:"project_uuid"`
	EnvironmentName string `json:"environment_name"`
	ServerUUID      string `json:"server_uuid"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	GitRepository   string `json:"git_repository"`
	GitBranch       string `json:"git_branch"`
	BuildPack       string `json:"build_pack"`
	PortsExposes    string `json:"ports_exposes"`
	BaseDirectory   string `json:"base_directory,omitempty"`
	IsStatic        bool   `json:"is_static,omitempty"`
	InstantDeploy   bool   `json:"instant_deploy"`
	Source          string `json:"-"`
	GitHubAppUUID   string `json:"github_app_uuid,omitempty"`
	PrivateKeyUUID  string `json:"private_key_uuid,omitempty"`

	// Build pack refinements; each applies to some packs only, and the
	// server ignores the rest. Locations are relative to the base directory
	// and start with a slash, as the server stores them.
	PublishDirectory      string          `json:"publish_directory,omitempty"`
	InstallCommand        string          `json:"install_command,omitempty"`
	BuildCommand          string          `json:"build_command,omitempty"`
	StartCommand          string          `json:"start_command,omitempty"`
	DockerfileLocation    string          `json:"dockerfile_location,omitempty"`
	DockerComposeLocation string          `json:"docker_compose_location,omitempty"`
	DockerComposeDomains  []ComposeDomain `json:"docker_compose_domains,omitempty"`
	HealthCheckEnabled    *bool           `json:"health_check_enabled,omitempty"`
}

// ComposeDomain assigns a domain to one service of a Compose application.
// Name is the service name in the compose file, Domain is Coolify's
// comma-separated URL list for it, and Redirect is "www", "non-www",
// "both", or empty, which keeps the service's current policy on an update.
type ComposeDomain struct {
	Name     string `json:"name"`
	Domain   string `json:"domain"`
	Redirect string `json:"redirect,omitempty"`
}

// GitHubApp is a GitHub App registered in Coolify. ID addresses the branch
// listing, which the server keys by row id; UUID is what application
// creation takes. The built-in "Public GitHub" source is listed with
// IsPublic set and has no installation to read repositories through. A
// system-wide app belongs to another team, which cannot list its branches
// but may create applications with it.
type GitHubApp struct {
	ID           int    `json:"id"`
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	Organization string `json:"organization"`
	HTMLURL      string `json:"html_url"`
	IsPublic     bool   `json:"is_public"`
	IsSystemWide bool   `json:"is_system_wide"`
	TeamID       int    `json:"team_id"`
}

// GitHubBranch is one branch of a repository as an installed GitHub App
// sees it.
type GitHubBranch struct {
	Name string `json:"name"`
}

// PrivateKey is an SSH key Coolify holds. The private half is never decoded
// into this type; PublicKey is what a Git host registers as a deploy key.
// IsGitRelated marks keys that belong to a GitHub or GitLab App.
type PrivateKey struct {
	ID           int    `json:"id"`
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	PublicKey    string `json:"public_key"`
	Fingerprint  string `json:"fingerprint"`
	IsGitRelated bool   `json:"is_git_related"`
}

// CreatedApplication is the server's answer to a creation: the new identity
// and the domains it assigned, which is a generated <uuid>.<wildcard> URL
// unless the request set its own.
type CreatedApplication struct {
	UUID    string `json:"uuid"`
	Domains string `json:"domains"`
}

// Team identifies the team an API token acts for.
type Team struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
