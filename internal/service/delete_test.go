package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeleteConfirmsBeforeItSendsAnythingAndKeepsTheBinding(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	options := DeleteOptions{Options: linkedOptions(t)}
	if _, err := app.Delete(context.Background(), options, nil, nil); !errors.Is(err, ErrInput) || f.calls["delete"] != 0 {
		t.Fatalf("noninteractive without --yes: err=%v deletes=%d", err, f.calls["delete"])
	}
	declined := func(context.Context, DeletePlan) (bool, error) { return false, nil }
	if _, err := app.Delete(context.Background(), options, declined, nil); !errors.Is(err, ErrCancelled) || f.calls["delete"] != 0 {
		t.Fatalf("declined: err=%v deletes=%d", err, f.calls["delete"])
	}
	var plan DeletePlan
	accepted := func(_ context.Context, p DeletePlan) (bool, error) { plan = p; return true, nil }
	var events []string
	result, err := app.Delete(context.Background(), options, accepted, func(e Event) error {
		events = append(events, e.Type+":"+e.Message)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The plan names the application as it is now, so the URLs it serves are
	// there to be recognized before the deletion is agreed to.
	if plan.Status != "running:healthy" || plan.Target.ApplicationUUID != "app-1" || plan.Target.Environment != "production" {
		t.Fatalf("plan %+v", plan)
	}
	if !reflect.DeepEqual(plan.URLs, []string{"https://app.example.com"}) || plan.KeepVolumes || plan.Unlink || plan.ConfigPath != "" {
		t.Fatalf("plan %+v", plan)
	}
	if result.Message != "Application deletion request queued." || f.calls["delete"] != 1 || !f.deletedVolumes {
		t.Fatalf("result=%+v calls=%v volumes=%v", result, f.calls, f.deletedVolumes)
	}
	if !reflect.DeepEqual(events, []string{"application:Application deletion request queued."}) {
		t.Fatalf("events %v", events)
	}
	// The binding is a local file the developer did not ask about, so it stays.
	if result.Unlinked != "" {
		t.Fatalf("unlinked %q", result.Unlinked)
	}
	if _, err := os.Stat(filepath.Join(options.CWD, "coolship.toml")); err != nil {
		t.Fatalf("binding was removed without --unlink: %v", err)
	}
}

func TestDeleteKeepsVolumesOnRequestAndRemovesTheBindingWithUnlink(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	options := DeleteOptions{Options: linkedOptions(t), Yes: true, KeepVolumes: true, Unlink: true}
	result, err := app.Delete(context.Background(), options, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.deletedVolumes {
		t.Fatal("--keep-volumes must reach the server as delete_volumes=false")
	}
	if !result.KeepVolumes {
		t.Fatalf("result %+v", result)
	}
	path := filepath.Join(options.CWD, "coolship.toml")
	if result.Unlinked != path {
		t.Fatalf("unlinked %q, want %q", result.Unlinked, path)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("binding still there: %v", err)
	}
}

func TestDeleteReportsAKeptBindingAsAWarningBecauseTheApplicationIsAlreadyGone(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	options := DeleteOptions{Options: linkedOptions(t), Yes: true, Unlink: true}
	// A read-only directory still reads the binding but refuses the removal,
	// so the failure lands after the application has already been deleted.
	path := filepath.Join(options.CWD, "coolship.toml")
	if err := os.Chmod(options.CWD, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(options.CWD, 0700) })
	var warnings []string
	result, err := app.Delete(context.Background(), options, nil, func(e Event) error {
		if e.Type == "warning" {
			warnings = append(warnings, e.Message)
		}
		return nil
	})
	// The application is gone, so the command reports what happened rather
	// than failing as if nothing had.
	if err != nil {
		t.Fatalf("a binding that cannot be removed must not fail the command: %v", err)
	}
	if f.calls["delete"] != 1 || result.Unlinked != "" {
		t.Fatalf("result=%+v calls=%v", result, f.calls)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "was deleted, but") || !strings.Contains(warnings[0], path) {
		t.Fatalf("warnings %v", warnings)
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != warnings[0] {
		t.Fatalf("result warnings %v", result.Warnings)
	}
}

func TestDeleteReportsARefusalAndLeavesTheBindingAlone(t *testing.T) {
	f := newBackend()
	f.deleteError = errors.New("403 Forbidden")
	app, _, _ := testApp(f)
	options := DeleteOptions{Options: linkedOptions(t), Yes: true, Unlink: true}
	result, err := app.Delete(context.Background(), options, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "delete application") {
		t.Fatalf("err %v", err)
	}
	if result.Unlinked != "" {
		t.Fatalf("a refused deletion must not remove the binding: %+v", result)
	}
	if _, statErr := os.Stat(filepath.Join(options.CWD, "coolship.toml")); statErr != nil {
		t.Fatalf("binding was removed although the server refused: %v", statErr)
	}
}

func TestDeleteStopsOnACancelledContextBeforeSending(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	ctx, cancel := context.WithCancel(context.Background())
	accepted := func(context.Context, DeletePlan) (bool, error) { cancel(); return true, nil }
	if _, err := app.Delete(ctx, DeleteOptions{Options: linkedOptions(t)}, accepted, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if f.calls["delete"] != 0 {
		t.Fatalf("a cancelled run must send nothing: %v", f.calls)
	}
}
