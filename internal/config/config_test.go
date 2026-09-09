package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/config"
)

const valid = `version = 1
[project]
context = "home"
project = "Personal"
environment = "production"
application = "web"
`

func TestRoundTripPreservesSelectorsAndPins(t *testing.T) {
	value, err := config.Parse([]byte(valid + `application_uuid = "app-123"`))
	if err != nil {
		t.Fatal(err)
	}
	if value.Project.Root != "." || value.Project.ApplicationUUID != "app-123" {
		t.Fatalf("unexpected binding: %#v", value.Project)
	}
	encoded, err := config.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := config.Parse(encoded)
	if err != nil || !config.Equal(value, decoded) {
		t.Fatalf("round trip: got %#v, error %v", decoded, err)
	}
}

func TestStrictSchema(t *testing.T) {
	tests := map[string]string{
		"unknown field":      valid + "aplication = 'typo'",
		"secret field":       valid + "token = 'synthetic-secret-do-not-report'",
		"future targets":     valid + "\n[apps.web]\nroot = '.'",
		"unknown version":    strings.Replace(valid, "version = 1", "version = 2", 1),
		"missing version":    strings.Replace(valid, "version = 1", "", 1),
		"invalid type":       strings.Replace(valid, "version = 1", "version = '1'", 1),
		"missing binding":    "version = 1",
		"missing selector":   strings.Replace(valid, "application = \"web\"", "application = ''", 1),
		"duplicate key":      valid + "application = 'other'",
		"root escapes":       valid + "root = '../outside'",
		"root absolute":      valid + "root = '/outside'",
		"root Windows drive": valid + `root = 'C:\outside'`,
		"root Windows path":  valid + `root = '..\outside'`,
		"malformed":          valid + "root = \"synthetic-secret-do-not-report",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse([]byte(input))
			if !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("expected configuration error, got %v", err)
			}
			if strings.Contains(err.Error(), "synthetic-secret-do-not-report") {
				t.Fatal("error discloses source value")
			}
		})
	}
}

func TestExplicitPinsCanDescribeUnnamedResources(t *testing.T) {
	_, err := config.Parse([]byte(`version = 1
[project]
project_uuid = "project-id"
environment_uuid = "environment-id"
application_uuid = "application-id"
`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestNamedTargetsRoundTripAndMixedFormIsRejected(t *testing.T) {
	named := `version = 1

[apps.web]
project = "Personal"
environment = "production"
application = "frontend"
root = "apps/web"

[apps.api]
project = "Personal"
environment = "production"
application = "backend"
`
	value, err := config.Parse([]byte(named))
	if err != nil {
		t.Fatal(err)
	}
	if !value.Named() || value.Project.IsSet() || value.Apps["api"].Root != "." || value.Apps["web"].Root != "apps/web" {
		t.Fatalf("parsed %+v", value)
	}
	encoded, err := config.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "[project]") || strings.Contains(string(encoded), "[apps]\n") {
		t.Fatalf("named form emitted a [project] table or an empty [apps] header:\n%s", encoded)
	}
	decoded, err := config.Parse(encoded)
	if err != nil || !config.Equal(value, decoded) {
		t.Fatalf("round trip: %v\n%s", err, encoded)
	}
	for name, input := range map[string]string{
		"mixed":         named + "\n[project]\nproject = \"P\"\nenvironment = \"E\"\napplication = \"A\"\n",
		"bad name":      "version = 1\n[apps.\"bad name\"]\nproject = \"P\"\nenvironment = \"E\"\napplication = \"A\"\n",
		"reserved":      "version = 1\n[apps.default]\nproject = \"P\"\nenvironment = \"E\"\napplication = \"A\"\n",
		"incomplete":    "version = 1\n[apps.web]\nproject = \"P\"\n",
		"escaping root": "version = 1\n[apps.web]\nproject = \"P\"\nenvironment = \"E\"\napplication = \"A\"\nroot = \"../x\"\n",
	} {
		if _, err := config.Parse([]byte(input)); !errors.Is(err, config.ErrInvalid) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}
