// Package models defines the small resource vocabulary shared by Coolship workflows.
package models

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
	UUID   string  `json:"deployment_uuid"`
	Status string  `json:"status"`
	Logs   *string `json:"logs,omitempty"`
}

type DeploymentReceipt struct {
	ResourceUUID   string `json:"resource_uuid"`
	DeploymentUUID string `json:"deployment_uuid"`
	Message        string `json:"message"`
}

type LogSnapshot struct {
	Logs string `json:"logs"`
}
