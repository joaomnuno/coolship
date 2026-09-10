package coolify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// ListDeployments reads the newest take rows of an application's deployment
// history. The server sends build logs in every row to a token that may read
// sensitive data; decoding into DeploymentRecord drops them, so they never
// leave this call.
func (c *Client) ListDeployments(ctx context.Context, uuid string, take int) (models.DeploymentPage, error) {
	if take < 1 {
		return models.DeploymentPage{}, errors.New("deployment history size must be at least 1")
	}
	var response struct {
		Count       *int                       `json:"count"`
		Deployments *[]models.DeploymentRecord `json:"deployments"`
	}
	err := c.request(ctx, http.MethodGet, []string{"deployments", "applications", uuid}, url.Values{"take": {strconv.Itoa(take)}}, nil, &response)
	if err != nil {
		return models.DeploymentPage{}, err
	}
	if response.Deployments == nil || response.Count == nil {
		return models.DeploymentPage{}, &ProtocolError{Endpoint: "/deployments/applications/{uuid}", Reason: "response omits the deployments array or count"}
	}
	return models.DeploymentPage{Total: *response.Count, Deployments: *response.Deployments}, nil
}

// StopApplication asks Coolify to stop the application's containers. The
// server queues the job and answers with a message; the application's status
// changes when the job runs. The server's docker_cleanup default (true, the
// same as its UI) is left in place.
func (c *Client) StopApplication(ctx context.Context, uuid string) (string, error) {
	var response struct {
		Message string `json:"message"`
	}
	if err := c.request(ctx, http.MethodPost, []string{"applications", uuid, "stop"}, nil, nil, &response); err != nil {
		return "", err
	}
	return response.Message, nil
}

// StartApplication queues a deployment through the start action, which is
// how Coolify brings a stopped application back. Force rebuilds without
// cache. Like Deploy, the POST is never retried, and a failure without an
// answer is uncertain.
func (c *Client) StartApplication(ctx context.Context, uuid string, force bool) (models.ActionReceipt, error) {
	body := struct {
		Force bool `json:"force"`
	}{Force: force}
	return c.action(ctx, uuid, "start", body)
}

// RestartApplication queues a restart-only deployment. Coolify skips the
// build when an image for the commit exists, except for Dockerfile and Docker
// image applications, which it deploys in full.
func (c *Client) RestartApplication(ctx context.Context, uuid string) (models.ActionReceipt, error) {
	return c.action(ctx, uuid, "restart", nil)
}

func (c *Client) action(ctx context.Context, uuid, action string, body any) (models.ActionReceipt, error) {
	if err := ctx.Err(); err != nil {
		return models.ActionReceipt{}, err
	}
	var receipt models.ActionReceipt
	err := c.request(ctx, http.MethodPost, []string{"applications", uuid, action}, nil, body, &receipt)
	if err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 && httpErr.StatusCode != http.StatusRequestTimeout {
			return models.ActionReceipt{}, err
		}
		return models.ActionReceipt{}, &UncertainSubmissionError{ResourceUUID: uuid, Err: err}
	}
	return receipt, nil
}

// CancelDeployment cancels a queued or in-progress deployment. The server
// refuses any other state with 400 and a message naming the current status,
// which the error carries.
func (c *Client) CancelDeployment(ctx context.Context, uuid string) (models.CancelReceipt, error) {
	var receipt models.CancelReceipt
	if err := c.request(ctx, http.MethodPost, []string{"deployments", uuid, "cancel"}, nil, nil, &receipt); err != nil {
		return models.CancelReceipt{}, err
	}
	return receipt, nil
}

func (c *Client) Logs(ctx context.Context, uuid string, lines int) (models.LogSnapshot, error) {
	if lines < 1 || lines > 10000 {
		return models.LogSnapshot{}, errors.New("log lines must be between 1 and 10000")
	}
	var response struct {
		Logs *string `json:"logs"`
	}
	// Coolify 4.3.18 answers 400 {"message": "Application is not running."}
	// when there is no container to read from; that one refusal is read so
	// it can be reported as such rather than as a bare status.
	err := c.requestExplaining(ctx, http.MethodGet, []string{"applications", uuid, "logs"}, url.Values{
		"lines": {strconv.Itoa(lines)}, "show_timestamps": {"true"},
	}, nil, &response, func(status int) bool { return status == http.StatusBadRequest })
	if err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusBadRequest {
			if strings.Contains(strings.ToLower(httpErr.Message), "not running") {
				return models.LogSnapshot{}, &NotRunningError{Message: httpErr.Message}
			}
			httpErr.Message = "" // any other 400 body is not an explanation to repeat
		}
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

func (c *Client) ListServers(ctx context.Context) ([]models.Server, error) {
	var servers []models.Server
	err := c.request(ctx, http.MethodGet, []string{"servers"}, nil, nil, &servers)
	return servers, err
}

// CreateProject creates a project; the server creates its production
// environment with it. A POST is never retried here.
func (c *Client) CreateProject(ctx context.Context, name, description string) (models.Project, error) {
	if strings.TrimSpace(name) == "" {
		return models.Project{}, errors.New("project name is required")
	}
	body := map[string]string{"name": name, "description": description}
	var response struct {
		UUID string `json:"uuid"`
	}
	if err := c.request(ctx, http.MethodPost, []string{"projects"}, nil, body, &response); err != nil {
		return models.Project{}, err
	}
	if response.UUID == "" {
		return models.Project{}, &ProtocolError{Endpoint: "/projects", Reason: "response omits the project uuid"}
	}
	return models.Project{UUID: response.UUID, Name: name}, nil
}

// CreateApplication creates an application from a repository through the
// endpoint the spec's Source selects: /applications/public, or the
// private-github-app and private-deploy-key variants. The server answers 201
// with the new uuid and its domains; a refusal carries the server's
// explanation. A POST is never retried here: if the request fails without an
// answer, the application may or may not exist.
func (c *Client) CreateApplication(ctx context.Context, spec models.ApplicationSpec) (models.CreatedApplication, error) {
	for _, required := range []struct{ name, value string }{
		{"project uuid", spec.ProjectUUID}, {"environment name", spec.EnvironmentName}, {"server uuid", spec.ServerUUID},
		{"name", spec.Name}, {"repository", spec.GitRepository}, {"branch", spec.GitBranch}, {"build pack", spec.BuildPack}, {"port", spec.PortsExposes},
	} {
		if strings.TrimSpace(required.value) == "" {
			return models.CreatedApplication{}, fmt.Errorf("application %s is required", required.name)
		}
	}
	endpoint := "public"
	switch spec.Source {
	case "", "public":
		if spec.GitHubAppUUID != "" || spec.PrivateKeyUUID != "" {
			return models.CreatedApplication{}, errors.New("a public application takes neither a GitHub App nor a private key")
		}
	case "github-app":
		endpoint = "private-github-app"
		if strings.TrimSpace(spec.GitHubAppUUID) == "" || spec.PrivateKeyUUID != "" {
			return models.CreatedApplication{}, errors.New("a GitHub App application requires the app uuid and no private key")
		}
	case "deploy-key":
		endpoint = "private-deploy-key"
		if strings.TrimSpace(spec.PrivateKeyUUID) == "" || spec.GitHubAppUUID != "" {
			return models.CreatedApplication{}, errors.New("a deploy key application requires the key uuid and no GitHub App")
		}
	default:
		return models.CreatedApplication{}, fmt.Errorf("unknown application source %q", spec.Source)
	}
	var response models.CreatedApplication
	if err := c.request(ctx, http.MethodPost, []string{"applications", endpoint}, nil, spec, &response); err != nil {
		return models.CreatedApplication{}, err
	}
	if response.UUID == "" {
		return models.CreatedApplication{}, &ProtocolError{Endpoint: "/applications/" + endpoint, Reason: "response omits the application uuid"}
	}
	return response, nil
}

// ListGitHubApps lists the GitHub Apps the token's team can use, including
// the built-in public source and apps other teams made system-wide.
func (c *Client) ListGitHubApps(ctx context.Context) ([]models.GitHubApp, error) {
	var apps []models.GitHubApp
	err := c.request(ctx, http.MethodGet, []string{"github-apps"}, nil, nil, &apps)
	return apps, err
}

// ListGitHubBranches lists the branches of one repository as the GitHub App
// sees them, which is also how the app's access to the repository is known:
// one it cannot reach is the server's 404. The app is addressed by its row
// id, which is how the server keys this route. The server relays GitHub's
// first page only, 30 branches by default. The app's whole repository
// listing (GET /github-apps/{id}/repositories) is deliberately not wrapped:
// the server pages through every installation repository to build it and
// times out on large installations, which is why Coolify's own creation
// path checks access per repository instead.
func (c *Client) ListGitHubBranches(ctx context.Context, appID int, owner, repo string) ([]models.GitHubBranch, error) {
	if appID < 0 {
		return nil, errors.New("GitHub App id must not be negative")
	}
	var response struct {
		Branches *[]models.GitHubBranch `json:"branches"`
	}
	if err := c.request(ctx, http.MethodGet, []string{"github-apps", strconv.Itoa(appID), "repositories", owner, repo, "branches"}, nil, nil, &response); err != nil {
		return nil, err
	}
	if response.Branches == nil {
		return nil, &ProtocolError{Endpoint: "/github-apps/{id}/repositories/{owner}/{repo}/branches", Reason: "response omits the branches array"}
	}
	return *response.Branches, nil
}

// ListPrivateKeys lists the team's SSH keys. The server includes the private
// halves when the token may read sensitive data; they are not decoded.
func (c *Client) ListPrivateKeys(ctx context.Context) ([]models.PrivateKey, error) {
	var keys []models.PrivateKey
	err := c.request(ctx, http.MethodGet, []string{"security", "keys"}, nil, nil, &keys)
	return keys, err
}

// CreatePrivateKey registers a private key the caller generated; the server
// derives the public half and refuses a key it already holds. It answers 201
// with the new uuid only. A POST is never retried here.
func (c *Client) CreatePrivateKey(ctx context.Context, name, description, privateKey string) (models.PrivateKey, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(privateKey) == "" {
		return models.PrivateKey{}, errors.New("a private key needs a name and the key itself")
	}
	body := struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		PrivateKey  string `json:"private_key"`
	}{Name: name, Description: description, PrivateKey: privateKey}
	var response struct {
		UUID string `json:"uuid"`
	}
	if err := c.request(ctx, http.MethodPost, []string{"security", "keys"}, nil, body, &response); err != nil {
		return models.PrivateKey{}, err
	}
	if response.UUID == "" {
		return models.PrivateKey{}, &ProtocolError{Endpoint: "/security/keys", Reason: "response omits the key uuid"}
	}
	return models.PrivateKey{UUID: response.UUID, Name: name, Description: description}, nil
}

// Team reads the team the token belongs to; it doubles as a credential check.
func (c *Client) Team(ctx context.Context) (models.Team, error) {
	var team models.Team
	err := c.request(ctx, http.MethodGet, []string{"teams", "current"}, nil, nil, &team)
	return team, err
}
