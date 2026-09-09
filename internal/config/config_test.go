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
	if err != nil || value != decoded {
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
