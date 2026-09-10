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
	// Logs is the build log document as the server sends it: a JSON array
	// encoded inside a JSON string. It is nil when the token cannot read
	// sensitive data. Decode it with ParseDeploymentLogs.
	Logs *string `json:"logs,omitempty"`
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

// Team identifies the team an API token acts for.
type Team struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
