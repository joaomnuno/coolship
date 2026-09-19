package cmd_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
)

func TestDeleteFlagsReachTheServiceAndTheResultNamesWhatIsGone(t *testing.T) {
	var seen service.DeleteOptions
	app := fakeApplication{del: func(_ context.Context, options service.DeleteOptions, confirm service.ConfirmDelete, _ service.Emitter) (service.DeleteResult, error) {
		seen = options
		if !options.Yes {
			if _, err := confirm(context.Background(), service.DeletePlan{Target: service.TargetInfo{Application: "api"}}); err != nil {
				return service.DeleteResult{}, err
			}
		}
		result := service.DeleteResult{Target: service.TargetInfo{Application: "api", ApplicationUUID: "app-1", Project: "Personal"},
			Message: "Application deletion request queued.", KeepVolumes: options.KeepVolumes}
		if options.Unlink {
			result.Unlinked = "/p/coolship.toml"
		}
		return result, nil
	}}
	// Deletion is irreversible, so a noninteractive run without --yes prints
	// nothing on stdout and fails as invalid input.
	out, _, err := execute(t, app, "delete")
	if !errors.Is(err, service.ErrInput) || out != "" {
		t.Fatalf("delete without --yes: out=%q err=%v", out, err)
	}
	out, _, err = execute(t, app, "delete", "--yes")
	if err != nil || out != "Deleted api (app-1) from Personal\n" {
		t.Fatalf("delete --yes: out=%q err=%v", out, err)
	}
	if !seen.Yes || seen.KeepVolumes || seen.Unlink {
		t.Fatalf("options %+v", seen)
	}
	out, _, err = execute(t, app, "delete", "--yes", "--keep-volumes", "--unlink")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Its volumes were kept.") || !strings.Contains(out, "Unlinked /p/coolship.toml") {
		t.Fatalf("out %q", out)
	}
	if !seen.KeepVolumes || !seen.Unlink {
		t.Fatalf("options %+v", seen)
	}
	// A target names the application the same way every other verb takes one.
	if _, _, err := execute(t, app, "delete", "web", "--yes"); err != nil {
		t.Fatal(err)
	}
	if seen.Target != "web" {
		t.Fatalf("target %q", seen.Target)
	}
	out, _, err = execute(t, app, "delete", "--yes", "--format", "json")
	if err != nil || !strings.Contains(out, `"message":"Application deletion request queued."`) {
		t.Fatalf("json: out=%q err=%v", out, err)
	}
}

func TestDeleteIsListedToMaintainAProjectAndNeverRunsWithoutTheService(t *testing.T) {
	out, _, err := execute(t, nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\n  delete ") {
		t.Fatalf("delete is missing from the grouped help: %q", out)
	}
	// It belongs with the other commands that tidy a project up, after doctor
	// and beside unlink, not among the ones that ship it.
	maintain := out[strings.Index(out, "Maintain"):]
	if doctor, del := strings.Index(maintain, "doctor"), strings.Index(maintain, "delete"); doctor < 0 || del < doctor {
		t.Fatalf("Maintain section orders delete wrongly: %q", maintain)
	}
	out, _, err = execute(t, nil, "help", "delete")
	if err != nil || !strings.Contains(out, "--keep-volumes") || !strings.Contains(out, "Nothing\nbrings it back.") {
		t.Fatalf("help delete: out=%q err=%v", out, err)
	}
}
