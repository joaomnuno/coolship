package coolify

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The testdata fixtures preserve the response shapes observed on Coolify 4.3.18
// with every identifier, hostname, and value replaced. They pin the decoding
// contract to what the server actually sends rather than to what its source
// suggests.

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func serveFixtures(t *testing.T, opts ...Option) (*Client, *http.Header) {
	t.Helper()
	var seen http.Header
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/applications/app-uuid-000000000000000":
			w.Write(fixture(t, "application.json"))
		case r.URL.Path == "/api/v1/applications/app-uuid-000000000000000/logs":
			w.Write(fixture(t, "application-logs.json"))
		case r.URL.Path == "/api/v1/deployments/deploy-uuid-00000000000000":
			w.Write(fixture(t, "deployment.json"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}, opts...)
	return client, &seen
}

func TestObservedDeploymentShape(t *testing.T) {
	client, _ := serveFixtures(t)
	deployment, err := client.GetDeployment(context.Background(), "deploy-uuid-00000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if deployment.UUID != "deploy-uuid-00000000000000" || deployment.Status != "finished" {
		t.Fatalf("unexpected deployment %+v", deployment)
	}
	// Build logs arrive as a JSON document encoded inside a JSON string.
	if deployment.Logs == nil {
		t.Fatal("deployment logs were present on the server but decoded as absent")
	}
	var entries []struct {
		Output string `json:"output"`
		Hidden bool   `json:"hidden"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal([]byte(*deployment.Logs), &entries); err != nil {
		t.Fatalf("deployment logs are not a JSON array: %v", err)
	}
	visible, hidden := 0, 0
	for _, entry := range entries {
		if entry.Hidden {
			hidden++
		} else {
			visible++
		}
	}
	if visible == 0 || hidden == 0 {
		t.Fatalf("fixture should carry both visible and hidden entries, got visible=%d hidden=%d", visible, hidden)
	}
}

func TestObservedRuntimeLogsOmitFinalNewline(t *testing.T) {
	client, _ := serveFixtures(t)
	snapshot, err := client.Logs(context.Background(), "app-uuid-000000000000000", 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(snapshot.Logs, "\n") {
		t.Fatal("fixture must preserve the missing final newline the server sends")
	}
	if strings.Count(snapshot.Logs, "\n") != 1 {
		t.Fatalf("expected two lines separated by one newline, got %q", snapshot.Logs)
	}
}

func TestObservedApplicationShape(t *testing.T) {
	client, _ := serveFixtures(t)
	application, err := client.GetApplication(context.Background(), "app-uuid-000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if application.UUID != "app-uuid-000000000000000" || application.Status != "running:healthy" || application.FQDN == "" {
		t.Fatalf("unexpected application %+v", application)
	}
}

func TestUserAgentIdentifiesCoolship(t *testing.T) {
	client, seen := serveFixtures(t)
	if _, err := client.GetApplication(context.Background(), "app-uuid-000000000000000"); err != nil {
		t.Fatal(err)
	}
	if got := seen.Get("User-Agent"); got != DefaultUserAgent {
		t.Fatalf("default User-Agent = %q", got)
	}
	client, seen = serveFixtures(t, WithUserAgent("coolship/1.2.3"))
	if _, err := client.GetApplication(context.Background(), "app-uuid-000000000000000"); err != nil {
		t.Fatal(err)
	}
	if got := seen.Get("User-Agent"); got != "coolship/1.2.3" {
		t.Fatalf("configured User-Agent = %q", got)
	}
	// A header-splitting value must not replace the identity.
	client, seen = serveFixtures(t, WithUserAgent("evil/1\r\nX-Injected: yes"))
	if _, err := client.GetApplication(context.Background(), "app-uuid-000000000000000"); err != nil {
		t.Fatal(err)
	}
	if got := seen.Get("User-Agent"); got != DefaultUserAgent || seen.Get("X-Injected") != "" {
		t.Fatalf("control characters accepted: ua=%q injected=%q", got, seen.Get("X-Injected"))
	}
}
