// Package models defines the small resource vocabulary shared by Coolship workflows.
package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Project struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
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
	FQDN   string `json:"fqdn,omitempty"`
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
// "non-www", or "both"; Force bypasses the server's in-use check.
type DomainUpdate struct {
	Domains  []string
	Redirect string
	Force    bool
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
