package coolify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The handlers answer as Coolify 4.3.18 was observed to: history as
// {count, deployments} with build logs for a sensitive-read token, actions
// with a message and the queued deployment's UUID, and cancellation with the
// status it set or a 400 naming the current one.
func TestLifecycleEndpoints(t *testing.T) {
	var bodies []map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/deployments/applications/app-1":
			if r.URL.Query().Get("take") != "2" {
				t.Errorf("take = %q", r.URL.Query().Get("take"))
			}
			fmt.Fprint(w, `{"count":14,"deployments":[
				{"id":154,"deployment_uuid":"d-new","pull_request_id":0,"force_rebuild":false,"commit":"0cd7c4a692347804dbd076a4d7e11c847e085473","status":"finished","is_webhook":false,"logs":"[{\"output\":\"secret build output\"}]","restart_only":false,"server_name":"Master","rollback":false,"commit_message":"cool container","is_api":true,"finished_at":"2026-09-10T11:37:36.000000Z","created_at":"2026-09-10T11:37:12.000000Z","updated_at":"2026-09-10T11:37:37.000000Z"},
				{"id":153,"deployment_uuid":"d-run","pull_request_id":7,"commit":"HEAD","status":"in_progress","finished_at":null,"created_at":"2026-09-10T11:40:00.000000Z","updated_at":"2026-09-10T11:40:00.000000Z"}
			]}`)
		case "GET /api/v1/deployments/applications/broken":
			fmt.Fprint(w, `{"message":"ok"}`)
		case "POST /api/v1/applications/app-1/stop":
			if r.URL.RawQuery != "" {
				t.Errorf("stop query = %q", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"message":"Application stopping request queued."}`)
		case "POST /api/v1/applications/app-1/start":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			bodies = append(bodies, body)
			if body["force"] == true {
				fmt.Fprint(w, `{"message":"Deployment already queued for this commit."}`)
				return
			}
			fmt.Fprint(w, `{"message":"Deployment request queued.","deployment_uuid":"d-start"}`)
		case "POST /api/v1/applications/app-1/restart":
			fmt.Fprint(w, `{"message":"Restart request queued.","deployment_uuid":"d-restart"}`)
		case "POST /api/v1/applications/gone/start":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Application not found."}`)
		case "POST /api/v1/applications/flaky/restart":
			w.WriteHeader(http.StatusBadGateway)
		case "POST /api/v1/deployments/d-run/cancel":
			fmt.Fprint(w, `{"message":"Deployment cancelled successfully.","deployment_uuid":"d-run","status":"cancelled-by-user"}`)
		case "POST /api/v1/deployments/d-new/cancel":
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"message":"Deployment cannot be cancelled. Current status: finished"}`)
		default:
			t.Errorf("unexpected endpoint %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	page, err := client.ListDeployments(ctx, "app-1", 2)
	if err != nil || page.Total != 14 || len(page.Deployments) != 2 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	first, second := page.Deployments[0], page.Deployments[1]
	if first.UUID != "d-new" || first.Status != "finished" || first.Commit != "0cd7c4a692347804dbd076a4d7e11c847e085473" || !first.IsAPI || first.FinishedAt != "2026-09-10T11:37:36.000000Z" || first.CommitMessage != "cool container" {
		t.Fatalf("first = %+v", first)
	}
	if second.PullRequest != 7 || second.FinishedAt != "" || second.Status != "in_progress" {
		t.Fatalf("second = %+v", second)
	}
	if data, _ := json.Marshal(page); strings.Contains(string(data), "secret build output") {
		t.Fatalf("history carries build logs: %s", data)
	}
	if _, err := client.ListDeployments(ctx, "broken", 1); err == nil || !strings.Contains(err.Error(), "omits the deployments array") {
		t.Fatalf("broken page: %v", err)
	}
	if _, err := client.ListDeployments(ctx, "app-1", 0); err == nil {
		t.Fatal("take 0 accepted")
	}
	message, err := client.StopApplication(ctx, "app-1")
	if err != nil || message != "Application stopping request queued." {
		t.Fatalf("stop = %q, %v", message, err)
	}
	receipt, err := client.StartApplication(ctx, "app-1", false)
	if err != nil || receipt.DeploymentUUID != "d-start" || receipt.Message != "Deployment request queued." {
		t.Fatalf("start = %+v, %v", receipt, err)
	}
	// The server's decline is a receipt without a UUID, not an error.
	receipt, err = client.StartApplication(ctx, "app-1", true)
	if err != nil || receipt.DeploymentUUID != "" || !strings.Contains(receipt.Message, "already queued") {
		t.Fatalf("declined start = %+v, %v", receipt, err)
	}
	if len(bodies) != 2 || bodies[0]["force"] != false || bodies[1]["force"] != true {
		t.Fatalf("start bodies %v", bodies)
	}
	receipt, err = client.RestartApplication(ctx, "app-1")
	if err != nil || receipt.DeploymentUUID != "d-restart" {
		t.Fatalf("restart = %+v, %v", receipt, err)
	}
	// A refusal carries the server's explanation and is certain; a server fault is not.
	var uncertain *UncertainSubmissionError
	_, err = client.StartApplication(ctx, "gone", false)
	if err == nil || errors.As(err, &uncertain) || !strings.Contains(err.Error(), "Application not found") {
		t.Fatalf("refused start: %v", err)
	}
	if _, err := client.RestartApplication(ctx, "flaky"); !errors.As(err, &uncertain) || uncertain.ResourceUUID != "flaky" {
		t.Fatalf("faulted restart: %v", err)
	}
	cancelled, err := client.CancelDeployment(ctx, "d-run")
	if err != nil || cancelled.Status != "cancelled-by-user" || cancelled.DeploymentUUID != "d-run" {
		t.Fatalf("cancel = %+v, %v", cancelled, err)
	}
	var httpErr *HTTPError
	if _, err := client.CancelDeployment(ctx, "d-new"); !errors.As(err, &httpErr) || httpErr.StatusCode != 400 || !strings.Contains(err.Error(), "Current status: finished") {
		t.Fatalf("refused cancel: %v", err)
	}
}
