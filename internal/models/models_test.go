package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestComposeDomainsDecodeFromEveryShapeTheServerSends(t *testing.T) {
	bot := ComposeDomain{Name: "fenix-bot", Domain: "https://fenix-bot.itrocas.com", Redirect: "non-www"}
	api := ComposeDomain{Name: "api", Domain: "https://api.example.com,https://www.api.example.com"}
	for _, test := range []struct {
		name     string
		document string
		want     ComposeDomains
	}{
		// Coolify 4.3.x: the map json_encoded inside a JSON string, slashes escaped.
		{"string", `"{\"fenix-bot\":{\"domain\":\"https:\\/\\/fenix-bot.itrocas.com\",\"redirect\":\"non-www\"},\"api\":{\"domain\":\"https:\\/\\/api.example.com,https:\\/\\/www.api.example.com\"}}"`, ComposeDomains{bot, api}},
		{"bare object", `{"fenix-bot":{"domain":"https://fenix-bot.itrocas.com","redirect":"non-www"},"api":{"domain":"https://api.example.com,https://www.api.example.com"}}`, ComposeDomains{bot, api}},
		{"older values are the domains themselves", `"{\"api\":\"https://api.example.com,https://www.api.example.com\"}"`, ComposeDomains{api}},
		{"null fields", `{"web":{"domain":null,"redirect":null}}`, ComposeDomains{{Name: "web"}}},
		{"request array", `[{"name":"api","domain":"https://api.example.com,https://www.api.example.com"}]`, ComposeDomains{api}},
		{"empty array as a string", `"[]"`, nil},
		{"empty object as a string", `"{}"`, nil},
		{"empty string", `""`, nil},
		{"null", `null`, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var application Application
			if err := json.Unmarshal([]byte(`{"uuid":"a1","fqdn":null,"build_pack":"dockercompose","docker_compose_domains":`+test.document+`}`), &application); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(application.ComposeDomains, test.want) {
				t.Fatalf("decoded %+v, want %+v", application.ComposeDomains, test.want)
			}
			if !application.IsCompose() || application.FQDN != "" {
				t.Fatalf("application %+v is not read as Compose", application)
			}
		})
	}
	// Absent means none, and the service order of the document is kept.
	var application Application
	if err := json.Unmarshal([]byte(`{"uuid":"a1","fqdn":"https://app.example.com"}`), &application); err != nil || application.ComposeDomains != nil || application.IsCompose() {
		t.Fatalf("plain application: %+v err=%v", application, err)
	}
	var ordered ComposeDomains
	if err := json.Unmarshal([]byte(`{"z":{"domain":"https://z.example.com"},"a":{"domain":"https://a.example.com"},"m":"https://m.example.com"}`), &ordered); err != nil {
		t.Fatal(err)
	}
	if names := []string{ordered[0].Name, ordered[1].Name, ordered[2].Name}; !reflect.DeepEqual(names, []string{"z", "a", "m"}) {
		t.Fatalf("order %v", names)
	}
	// A listing without the build pack still tells a Compose application by its map.
	if !(Application{ComposeDomains: ordered}).IsCompose() || (Application{FQDN: "https://x.example.com", ComposeDomains: ordered}).IsCompose() {
		t.Fatal("IsCompose without a build pack")
	}
}

func TestComposeDomainsRefuseWhatIsNotAMap(t *testing.T) {
	for _, document := range []string{`"not json"`, `"{\"web\":42}"`, `42`, `{"web":{"domain":42}}`, `"{\"web\":{\"domain\":\"x\"}"`} {
		var domains ComposeDomains
		err := json.Unmarshal([]byte(document), &domains)
		if err == nil || !strings.Contains(err.Error(), "docker_compose_domains") {
			t.Errorf("%s decoded to %+v with err %v", document, domains, err)
		}
	}
}
