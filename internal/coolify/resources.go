package coolify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/joaomnuno/coolship/internal/models"
)

func (c *Client) ListProjects(ctx context.Context) ([]models.Project, error) {
	var projects []models.Project
	err := c.request(ctx, http.MethodGet, []string{"projects"}, nil, nil, &projects)
	return projects, err
}

func (c *Client) ListEnvironments(ctx context.Context, projectUUID string) ([]models.Environment, error) {
	var environments []models.Environment
	err := c.request(ctx, http.MethodGet, []string{"projects", projectUUID, "environments"}, nil, nil, &environments)
	return environments, err
}

func (c *Client) GetEnvironment(ctx context.Context, projectUUID, environmentUUID string) (models.Environment, error) {
	var environment models.Environment
	err := c.request(ctx, http.MethodGet, []string{"projects", projectUUID, environmentUUID}, nil, nil, &environment)
	return environment, err
}

func (c *Client) GetApplication(ctx context.Context, uuid string) (models.Application, error) {
	var application models.Application
	err := c.request(ctx, http.MethodGet, []string{"applications", uuid}, nil, nil, &application)
	return application, err
}

func (c *Client) Deploy(ctx context.Context, request models.DeployRequest) ([]models.DeploymentReceipt, error) {
	uuid := request.ApplicationUUID
	// The server treats a comma-delimited UUID string as multiple deployments.
	if uuid == "" || strings.Contains(uuid, ",") || strings.IndexFunc(uuid, unicode.IsSpace) >= 0 || strings.IndexFunc(uuid, unicode.IsControl) >= 0 {
		return nil, errors.New("deployment requires exactly one nonempty application UUID")
	}
	if request.PullRequest < 0 {
		return nil, errors.New("pull request number must be positive")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body := struct {
		UUID        string `json:"uuid"`
		Force       bool   `json:"force"`
		PullRequest int    `json:"pr,omitempty"`
	}{UUID: uuid, Force: request.Force, PullRequest: request.PullRequest}
	var response struct {
		Deployments *[]models.DeploymentReceipt `json:"deployments"`
	}
	err := c.request(ctx, http.MethodPost, []string{"deploy"}, nil, body, &response)
	if err == nil && response.Deployments == nil {
		err = &ProtocolError{Endpoint: "/deploy", Reason: "response omits the deployments array"}
	}
	if err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 && httpErr.StatusCode != http.StatusRequestTimeout {
			return nil, err
		}
		return nil, &UncertainSubmissionError{ResourceUUID: uuid, Err: err}
	}
	return *response.Deployments, nil
}

func (c *Client) GetDeployment(ctx context.Context, uuid string) (models.Deployment, error) {
	var deployment models.Deployment
	err := c.request(ctx, http.MethodGet, []string{"deployments", uuid}, nil, nil, &deployment)
	return deployment, err
}

func (c *Client) Logs(ctx context.Context, uuid string, lines int) (models.LogSnapshot, error) {
	if lines < 1 || lines > 10000 {
		return models.LogSnapshot{}, errors.New("log lines must be between 1 and 10000")
	}
	var response struct {
		Logs *string `json:"logs"`
	}
	err := c.request(ctx, http.MethodGet, []string{"applications", uuid, "logs"}, url.Values{
		"lines": {strconv.Itoa(lines)}, "show_timestamps": {"true"},
	}, nil, &response)
	if err != nil {
		return models.LogSnapshot{}, err
	}
	if response.Logs == nil {
		return models.LogSnapshot{}, &ProtocolError{Endpoint: "/applications/{uuid}/logs", Reason: "response omits runtime logs"}
	}
	return models.LogSnapshot{Logs: *response.Logs}, nil
}

// Version reads the server version. Coolify 4.3.18 answers with plain text
// (and an HTML content type), so this does not go through JSON decoding.
func (c *Client) Version(ctx context.Context) (string, error) {
	data, endpoint, err := c.fetch(ctx, http.MethodGet, []string{"version"}, nil, nil)
	if err != nil {
		return "", err
	}
	version := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if version == "" || len(version) > 64 || !unicode.IsDigit(rune(version[0])) ||
		strings.IndexFunc(version, func(r rune) bool { return r < ' ' || r > '~' }) >= 0 {
		return "", &ProtocolError{Endpoint: endpoint, Reason: "response is not a version string"}
	}
	return version, nil
}

func (c *Client) ListEnvironmentVariables(ctx context.Context, uuid string) ([]models.EnvironmentVariable, error) {
	var variables []models.EnvironmentVariable
	err := c.request(ctx, http.MethodGet, []string{"applications", uuid, "envs"}, nil, nil, &variables)
	return variables, err
}

// UpsertEnvironmentVariables creates or updates each item within its scope.
// The server matches on key and is_preview; a PATCH is never retried here.
func (c *Client) UpsertEnvironmentVariables(ctx context.Context, uuid string, items []models.EnvironmentVariableInput) error {
	if len(items) == 0 {
		return nil
	}
	body := struct {
		Data []models.EnvironmentVariableInput `json:"data"`
	}{Data: items}
	var response json.RawMessage
	return c.request(ctx, http.MethodPatch, []string{"applications", uuid, "envs", "bulk"}, nil, body, &response)
}

func (c *Client) DeleteEnvironmentVariable(ctx context.Context, uuid, variableUUID string) error {
	var response json.RawMessage
	return c.request(ctx, http.MethodDelete, []string{"applications", uuid, "envs", variableUUID}, nil, nil, &response)
}

// UpdateApplicationDomains sends the full domain list; the server treats it as
// a replacement. A PATCH is never retried here.
func (c *Client) UpdateApplicationDomains(ctx context.Context, uuid string, update models.DomainUpdate) error {
	body := map[string]any{"domains": strings.Join(update.Domains, ",")}
	if update.Redirect != "" {
		body["redirect"] = update.Redirect
	}
	if update.Force {
		body["force_domain_override"] = true
	}
	var response json.RawMessage
	return c.request(ctx, http.MethodPatch, []string{"applications", uuid}, nil, body, &response)
}
