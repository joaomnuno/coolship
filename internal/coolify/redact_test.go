package coolify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRedactBodyMasksSecretFieldsAndKeepsTheRest(t *testing.T) {
	cases := map[string]string{
		// env push and env set send values; env pull reads value and real_value.
		`{"data":[{"key":"DB_URL","value":"postgres://u:p@h/db","is_preview":false},{"key":"EMPTY","value":""}]}`: `{"data":[{"key":"DB_URL","value":"[redacted]","is_preview":false},{"key":"EMPTY","value":""}]}`,
		`[{"uuid":"e1","key":"TOKEN","value":"abc","real_value":"abc","is_shown_once":null}]`:                     `[{"uuid":"e1","key":"TOKEN","value":"[redacted]","real_value":"[redacted]","is_shown_once":null}]`,
		// init --create-deploy-key sends the private key; a reference to it stays.
		`{"name":"k","private_key":"-----BEGIN OPENSSH PRIVATE KEY-----\nxyz\n-----END OPENSSH PRIVATE KEY-----\n"}`:               `{"name":"k","private_key":"[redacted]"}`,
		`{"private_key_uuid":"pk-1","github_app_id":3}`:                                                                            `{"private_key_uuid":"pk-1","github_app_id":3}`,
		`{"client_secret":"s","manual_webhook_secret_github":"w","http_basic_auth_password":"p","nested":{"value":{"a":[1,"b"]}}}`: `{"client_secret":"[redacted]","manual_webhook_secret_github":"[redacted]","http_basic_auth_password":"[redacted]","nested":{"value":{"a":["[redacted]","[redacted]"]}}}`,
		`{"message":"bad <branch> & more","count":12345678901234567890}`:                                                           `{"message":"bad <branch> & more","count":12345678901234567890}`,
		`  {"version":"4.3.18"}` + "\n": `{"version":"4.3.18"}`,
		"<html>Server Error</html>":     "<html>Server Error</html>",
		`{"value":"cut sho`:             withheldBody,
	}
	for body, want := range cases {
		if got := string(redactBody([]byte(body))); got != want {
			t.Errorf("redactBody(%q)\n got %s\nwant %s", body, got, want)
		}
	}
}

// TestTraceRedactsSecretsUnlessUnredacted checks that a traced environment
// variable value never reaches the trace by default, and does when asked.
func TestTraceRedactsSecretsUnlessUnredacted(t *testing.T) {
	for _, unredacted := range []bool{false, true} {
		t.Run(fmt.Sprintf("unredacted=%t", unredacted), func(t *testing.T) {
			var traced Exchange
			options := []Option{WithTrace(func(e Exchange) { traced = e })}
			if unredacted {
				options = append(options, WithUnredactedTrace())
			}
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `[{"key":"API_KEY","value":"remote-secret"}]`)
			}, options...)
			data, _, err := client.fetch(context.Background(), http.MethodPost, []string{"envs"}, nil, map[string]string{"key": "API_KEY", "value": "local-secret"})
			if err != nil || !strings.Contains(string(data), "remote-secret") {
				t.Fatalf("the caller must get the body unchanged: %q %v", data, err)
			}
			for _, secret := range []string{"local-secret", "remote-secret"} {
				if got := strings.Contains(string(traced.RequestBody)+string(traced.ResponseBody), secret); got != unredacted {
					t.Errorf("%s in trace = %t; want %t (%s / %s)", secret, got, unredacted, traced.RequestBody, traced.ResponseBody)
				}
			}
		})
	}
}
