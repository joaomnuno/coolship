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

type DeploymentReceipt struct {
	ResourceUUID   string `json:"resource_uuid"`
	DeploymentUUID string `json:"deployment_uuid"`
	Message        string `json:"message"`
}

type LogSnapshot struct {
	Logs string `json:"logs"`
}
