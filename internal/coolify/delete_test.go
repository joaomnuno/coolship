package coolify

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The handlers answer as Coolify 4.3.23 was observed to: the application is
// deleted at once and the rest queued, with the four cleanup choices read from
// the query and defaulting to true.
func TestDeleteApplicationSendsTheVolumeChoiceAndIsNeverRetried(t *testing.T) {
	var queries []string
	attempts := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "DELETE /api/v1/applications/app-1":
			queries = append(queries, r.URL.RawQuery)
			fmt.Fprint(w, `{"message":"Application deletion request queued."}`)
		case "DELETE /api/v1/applications/member":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"This action is unauthorized."}`)
		case "DELETE /api/v1/applications/flaky":
			attempts++
			w.WriteHeader(http.StatusBadGateway)
		default:
			t.Errorf("unexpected endpoint %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	// The server's own defaults delete everything a deleted application owned,
	// so keeping the volumes is the only thing that has to be said.
	message, err := client.DeleteApplication(ctx, "app-1", true)
	if err != nil || message != "Application deletion request queued." {
		t.Fatalf("delete = %q, %v", message, err)
	}
	if _, err := client.DeleteApplication(ctx, "app-1", false); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[0] != "" || queries[1] != "delete_volumes=false" {
		t.Fatalf("queries %q", queries)
	}
	// Deletion needs a team administrator, which the refusal explains.
	_, err = client.DeleteApplication(ctx, "member", true)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("refusal: %v", err)
	}
	// A DELETE is a mutation: a server fault is reported, not sent again.
	if _, err := client.DeleteApplication(ctx, "flaky", true); err == nil {
		t.Fatal("server fault accepted")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want one", attempts)
	}
}
