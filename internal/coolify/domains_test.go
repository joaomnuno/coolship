package coolify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/joaomnuno/coolship/internal/models"
)

// A Compose application on Coolify 4.3.x has fqdn null and its domains in
// docker_compose_domains, a JSON object keyed by service name encoded inside
// a JSON string; the update takes the same map as an array of name, domain,
// and redirect under the same field, since it refuses domains for it.
func TestComposeDomainsAreReadAndReplacedPerService(t *testing.T) {
	var patches []map[string]json.RawMessage
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/applications/compose":
			fmt.Fprint(w, `{"uuid":"compose","name":"fenix-bot","status":"running:healthy","fqdn":null,"build_pack":"dockercompose",
				"docker_compose_domains":"{\"fenix-bot\":{\"domain\":\"https:\\/\\/fenix-bot.itrocas.com\",\"redirect\":\"non-www\"}}"}`)
		case "GET /api/v1/applications/fresh":
			fmt.Fprint(w, `{"uuid":"fresh","name":"new","status":"exited:unhealthy","fqdn":null,"build_pack":"dockercompose","docker_compose_domains":null}`)
		case "PATCH /api/v1/applications/compose", "PATCH /api/v1/applications/plain":
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			patches = append(patches, body)
			fmt.Fprint(w, `{"uuid":"compose"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	application, err := client.GetApplication(context.Background(), "compose")
	if err != nil {
		t.Fatal(err)
	}
	want := models.ComposeDomains{{Name: "fenix-bot", Domain: "https://fenix-bot.itrocas.com", Redirect: "non-www"}}
	if application.FQDN != "" || !application.IsCompose() || !reflect.DeepEqual(application.ComposeDomains, want) {
		t.Fatalf("application = %+v", application)
	}
	fresh, err := client.GetApplication(context.Background(), "fresh")
	if err != nil || fresh.ComposeDomains != nil || !fresh.IsCompose() {
		t.Fatalf("fresh = %+v err=%v", fresh, err)
	}

	err = client.UpdateApplicationDomains(context.Background(), "compose", models.DomainUpdate{
		Services: []models.ComposeDomain{{Name: "fenix-bot", Domain: "https://bot.example.com,https://www.bot.example.com", Redirect: "non-www"}, {Name: "api", Domain: "https://api.example.com"}},
		Force:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.UpdateApplicationDomains(context.Background(), "plain", models.DomainUpdate{Domains: []string{"https://app.example.com", "https://www.example.com"}, Redirect: "www"})
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 {
		t.Fatalf("patches = %v", patches)
	}
	compose := string(patches[0]["docker_compose_domains"])
	if compose != `[{"name":"fenix-bot","domain":"https://bot.example.com,https://www.bot.example.com","redirect":"non-www"},{"name":"api","domain":"https://api.example.com"}]` ||
		string(patches[0]["force_domain_override"]) != "true" || patches[0]["domains"] != nil || patches[0]["redirect"] != nil {
		t.Errorf("compose patch = %s", patches[0])
	}
	if string(patches[1]["domains"]) != `"https://app.example.com,https://www.example.com"` || string(patches[1]["redirect"]) != `"www"` ||
		patches[1]["docker_compose_domains"] != nil || patches[1]["force_domain_override"] != nil {
		t.Errorf("plain patch = %s", patches[1])
	}
}
