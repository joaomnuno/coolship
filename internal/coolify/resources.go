package coolify

import (
	"context"
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

func (c *Client) Deploy(ctx context.Context, uuid string, force bool) ([]models.DeploymentReceipt, error) {
	// The server treats a comma-delimited UUID string as multiple deployments.
	if uuid == "" || strings.Contains(uuid, ",") || strings.IndexFunc(uuid, unicode.IsSpace) >= 0 || strings.IndexFunc(uuid, unicode.IsControl) >= 0 {
		return nil, errors.New("deployment requires exactly one nonempty application UUID")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body := struct {
		UUID  string `json:"uuid"`
		Force bool   `json:"force"`
	}{UUID: uuid, Force: force}
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
