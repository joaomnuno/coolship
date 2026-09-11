package service

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/models"
)

// stage abbreviates an expected stage event.
func stage(name, status string) Event { return Event{Type: "stage", Stage: name, Status: status} }

// TestStageTrackerReadsCoolifyMarkers feeds real build log lines, as
// Coolify 4.3.18's ApplicationDeploymentJob writes them, through one tracker
// per case and checks the transitions, each at most once.
func TestStageTrackerReadsCoolifyMarkers(t *testing.T) {
	for _, test := range []struct {
		name  string
		lines []string
		want  []Event
	}{
		{"dockerfile deployment", []string{
			"Starting deployment of joaomnuno/example-coolify-project:main to Master Ubuntu.",
			"Preparing container with helper image: ghcr.io/coollabsio/coolify-helper:1.0.11",
			"Building docker image started.",
			"To check the current progress, click on Show Debug Logs.",
			"Building docker image completed.",
			"Rolling update started.",
			"New container started.",
			"Waiting for healthcheck to pass on the new container.",
			`Attempt 2 of 10 | Healthcheck status: "healthy"`,
			"New container is healthy.",
			"Removing old containers.",
			"Rolling update completed.",
		}, []Event{
			stage(StageBuild, StageStarted), stage(StageBuild, StageDone),
			stage(StageRollingUpdate, StageStarted),
			stage(StageContainer, StageStarted), stage(StageContainer, StageDone),
			stage(StageCleanup, StageStarted), stage(StageCleanup, StageDone),
			stage(StageRollingUpdate, StageDone),
		}},
		{"railpack build with trailing detail", []string{
			"  Building docker image with Railpack.  ",
			"Building docker image completed. (cached layers: 12)",
		}, []Event{stage(StageBuild, StageStarted), stage(StageBuild, StageDone)}},
		{"compose goes from pulling to starting", []string{
			"Pulling & building required images.",
			"New container started.",
			"New container is healthy.",
		}, []Event{
			stage(StageBuild, StageStarted), stage(StageBuild, StageDone),
			stage(StageContainer, StageStarted), stage(StageContainer, StageDone),
		}},
		{"container without a health check is accepted by the rolling update", []string{
			"Rolling update started.",
			"New container started.",
			"Rolling update completed.",
		}, []Event{
			stage(StageRollingUpdate, StageStarted),
			stage(StageContainer, StageStarted),
			stage(StageContainer, StageDone), stage(StageRollingUpdate, StageDone),
		}},
		{"unhealthy container fails the deployment", []string{
			"Rolling update started.",
			"New container started.",
			"New container is not healthy, rolling back to the old container.",
			"Rolling update failed (container unhealthy). Reverting to the old version.",
			"Deployment failed. Removing the new version of your application.",
		}, []Event{
			stage(StageRollingUpdate, StageStarted),
			stage(StageContainer, StageStarted),
			stage(StageContainer, StageFailed),
			stage(StageRollingUpdate, StageFailed),
		}},
		{"build failure fails the open stage", []string{
			"Building docker image started.",
			"ERROR: failed to solve: process did not complete successfully",
			"Deployment failed. Removing the new version of your application.",
		}, []Event{stage(StageBuild, StageStarted), stage(StageBuild, StageFailed)}},
		{"unhealthy after healthy is ignored", []string{
			"New container started.",
			"New container is healthy.",
			"New container is unhealthy.",
		}, []Event{stage(StageContainer, StageStarted), stage(StageContainer, StageDone)}},
		{"an end without a start implies the start", []string{
			"Building docker image completed.",
		}, []Event{stage(StageBuild, StageStarted), stage(StageBuild, StageDone)}},
		{"repeated starts count once", []string{
			"Building docker image started.",
			"Building docker image started.",
		}, []Event{stage(StageBuild, StageStarted)}},
		{"ordinary lines imply nothing", []string{
			"Cloning repository.",
			"Removing old containers is done.",
			"Deployment failed: it never began",
			"",
		}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var tracker stageTracker
			var got []Event
			for _, line := range test.lines {
				got = append(got, tracker.observe(line)...)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("events\n got %v\nwant %v", got, test.want)
			}
		})
	}
}

// TestDeploymentEmitsStagesBetweenBuildLogs checks the order a consumer sees:
// each marker line closes a build event and the stage events it implies
// follow it, before the next lines, and every stage event names the
// deployment.
func TestDeploymentEmitsStagesBetweenBuildLogs(t *testing.T) {
	entry := func(output string) string {
		return `{"command":null,"output":"` + output + `\n","type":"stdout","hidden":false}`
	}
	first := "[" + strings.Join([]string{entry("Starting deployment."), entry("Building docker image started."), entry("Step 1/4")}, ",") + "]"
	second := first[:len(first)-1] + "," + strings.Join([]string{entry("Building docker image completed."), entry("Rolling update started."), entry("New container started.")}, ",") + "]"
	f := newBackend()
	f.deployments = []models.Deployment{
		{UUID: "deploy-1", Status: "in_progress", Logs: &first},
		{UUID: "deploy-1", Status: "in_progress", Logs: &second},
		{UUID: "deploy-1", Status: "finished", Logs: &second},
	}
	app, _, _ := testApp(f)
	var got []string
	_, err := app.Deploy(context.Background(), DeployOptions{Options: linkedOptions(t)}, func(e Event) error {
		switch e.Type {
		case "build":
			got = append(got, "build:"+strings.ReplaceAll(e.Logs, "\n", "|"))
		case "stage":
			if e.DeploymentUUID != "deploy-1" {
				t.Fatalf("stage event without the deployment: %+v", e)
			}
			got = append(got, "stage:"+e.Stage+":"+e.Status)
		case "deployment":
			got = append(got, "deployment:"+e.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"deployment:queued",
		"build:Starting deployment.|Building docker image started.|",
		"stage:build:started",
		"build:Step 1/4|",
		"deployment:in_progress",
		"build:Building docker image completed.|",
		"stage:build:done",
		"build:Rolling update started.|",
		"stage:rolling update:started",
		"build:New container started.|",
		"stage:container:started",
		"deployment:finished",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %q\nwant %q", got, want)
	}
}
