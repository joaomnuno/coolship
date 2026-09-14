package service

import "strings"

// Stage names, in the order a deployment passes through them. A stage is one
// step of a deployment that Coolify names in its build log (CONTEXT.md):
// the image build, the rolling update, the new container coming up and
// passing its health check, and the removal of the old containers.
const (
	StageBuild         = "build"
	StageRollingUpdate = "rolling update"
	StageContainer     = "container"
	StageCleanup       = "cleanup"
)

// DeploymentStages lists every stage in display order, so a view can show
// the ones not reached yet.
var DeploymentStages = []string{StageBuild, StageRollingUpdate, StageContainer, StageCleanup}

// ParentStage names the stage another one runs inside, or "" for a stage of
// its own. In Coolify's rolling update the new container comes up and the old
// ones are removed between "Rolling update started." and "Rolling update
// completed.", so container and cleanup are its children. A deployment with
// no rolling update (compose, mapped ports) still reports them on their own.
func ParentStage(stage string) string {
	switch stage {
	case StageContainer, StageCleanup:
		return StageRollingUpdate
	}
	return ""
}

// Stage statuses carried by Event.Status when Event.Type is "stage".
const (
	StageStarted = "started"
	StageDone    = "done"
	StageFailed  = "failed"
	// StageSkipped is a stage Coolify said it would not run, such as a build
	// skipped because an image for the same commit already exists. The event
	// carries the reason in Message.
	StageSkipped = "skipped"
)

// stageMarker is one fixed line Coolify's ApplicationDeploymentJob writes
// into the build log and what it says about a stage. Matching is by prefix
// on the trimmed line, so trailing detail (a reason, a count) does not
// matter, and by suffix for a line that starts with variable detail.
type stageMarker struct {
	prefix string
	suffix string
	reason string // the Message of a skipped stage's event
	stage  string // the stage the line is about; empty for a line about every open stage
	status string
	closes string // a stage the line also finishes when it is still open
	// instant marks a stage that starts and finishes in this one line.
	instant bool
}

var stageMarkers = []stageMarker{
	{prefix: "Building docker image started.", stage: StageBuild, status: StageStarted},
	{prefix: "Building docker image with Railpack.", stage: StageBuild, status: StageStarted},
	{prefix: "Pulling & building required images.", stage: StageBuild, status: StageStarted},
	{prefix: "Building docker image completed.", stage: StageBuild, status: StageDone},
	// "Image found (<image>) with the same Git Commit SHA. Build step
	// skipped." and its "No build configuration changed & image found"
	// variant: the image is reused and the job goes to the rolling update.
	{suffix: "Build step skipped.", stage: StageBuild, status: StageSkipped, reason: "cached image"},
	{prefix: "Rolling update started.", stage: StageRollingUpdate, status: StageStarted},
	// A container that came up without a health check is accepted when the
	// rolling update completes; Coolify writes no healthy line for it.
	{prefix: "Rolling update completed.", stage: StageRollingUpdate, status: StageDone, closes: StageContainer},
	{prefix: "Rolling update failed (", stage: StageRollingUpdate, status: StageFailed},
	// A compose deployment can go from pulling straight to starting, with no
	// build-completed line; the new container finishes an open build.
	{prefix: "New container started.", stage: StageContainer, status: StageStarted, closes: StageBuild},
	{prefix: "New container is healthy.", stage: StageContainer, status: StageDone},
	{prefix: "New container is unhealthy.", stage: StageContainer, status: StageFailed},
	{prefix: "New container is not healthy, rolling back to the old container.", stage: StageContainer, status: StageFailed},
	{prefix: "Removing old containers.", stage: StageCleanup, status: StageStarted, instant: true},
	{prefix: "Deployment failed. Removing the new version of your application.", status: StageFailed},
}

// stageTracker turns the build log's marker lines into stage events, at most
// one per transition: a stage starts once and ends once, done or failed, and
// later markers for an ended stage are ignored. It is domain knowledge about
// Coolify's job, not presentation; the renderer decides how a stage looks.
type stageTracker struct {
	state map[string]string // stage → last status emitted
}

// observe reads one visible log line and returns the stage events it implies,
// in order. Most lines imply none.
func (t *stageTracker) observe(line string) []Event {
	line = strings.TrimSpace(line)
	for _, marker := range stageMarkers {
		if !strings.HasPrefix(line, marker.prefix) || !strings.HasSuffix(line, marker.suffix) {
			continue
		}
		if marker.stage == "" {
			return t.failOpen()
		}
		var events []Event
		if marker.closes != "" {
			events = append(events, t.transition(marker.closes, StageDone, false)...)
		}
		for _, event := range t.transition(marker.stage, marker.status, true) {
			if event.Status == StageSkipped {
				event.Message = marker.reason
			}
			events = append(events, event)
		}
		if marker.instant {
			events = append(events, t.transition(marker.stage, StageDone, false)...)
		}
		return events
	}
	return nil
}

// transition moves stage to status and returns the events that describes.
// An end (done or failed) for a stage that never started implies the start
// when implied is set, so the stage is still shown; closing another stage
// that never started implies nothing. Only a stage not reached yet can be
// skipped, and a skipped stage has ended.
func (t *stageTracker) transition(stage, status string, implied bool) []Event {
	if t.state == nil {
		t.state = map[string]string{}
	}
	current := t.state[stage]
	if current == StageDone || current == StageFailed || current == StageSkipped {
		return nil
	}
	if status == StageSkipped {
		if current != "" {
			return nil
		}
		t.state[stage] = StageSkipped
		return []Event{{Type: "stage", Stage: stage, Status: StageSkipped}}
	}
	if status == StageStarted {
		if current == StageStarted {
			return nil
		}
		t.state[stage] = StageStarted
		return []Event{{Type: "stage", Stage: stage, Status: StageStarted}}
	}
	var events []Event
	if current == "" {
		if !implied {
			return nil
		}
		events = append(events, Event{Type: "stage", Stage: stage, Status: StageStarted})
	}
	t.state[stage] = status
	return append(events, Event{Type: "stage", Stage: stage, Status: status})
}

// failOpen marks every open stage failed, in display order.
func (t *stageTracker) failOpen() []Event {
	var events []Event
	for _, stage := range DeploymentStages {
		if t.state[stage] == StageStarted {
			events = append(events, t.transition(stage, StageFailed, false)...)
		}
	}
	return events
}
