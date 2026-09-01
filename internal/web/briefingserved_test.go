package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
)

// The briefing, drawn from a real control plane rather than from jobs written by hand. What a table
// test cannot say is that the page reads what the system actually holds after the system has run
// something.

// theRepository and thePullRequest are a job that ends in a repository, and the address its session
// answers with. The system reads the address off the answer rather than believing a report of one.
const (
	theRepository  = "atlantic-blue/quay-crew"
	thePullRequest = "https://github.com/atlantic-blue/quay-crew/pull/454"
)

// TestTheFrontDoorNoLongerListsSessions is the way off the old page, tested beside the way onto the
// new one. The listing is still there and it has lost the front door, so an operator who opens the
// tab is answered rather than shown the question three surfaces already answer.
func TestTheFrontDoorNoLongerListsSessions(t *testing.T) {
	client := aSystem(t)
	session := dispatch(t, client, projectOf(t, client), "", "when is the electricity bill due")

	front, status := get(t, client, "/")
	if status != http.StatusOK {
		t.Fatalf("the front door answered %d", status)
	}
	if strings.Contains(front, `<li class="session">`) {
		t.Errorf("the front door is still the session listing:\n%s", front)
	}
	if !strings.Contains(front, "waiting on you") || !strings.Contains(front, "produced") {
		t.Errorf("the front door does not answer the three questions:\n%s", front)
	}

	listing, status := get(t, client, "/sessions")
	if status != http.StatusOK {
		t.Fatalf("the session listing answered %d", status)
	}
	if !strings.Contains(listing, session.GetId()) {
		t.Errorf("the session listing lost the session it is for:\n%s", listing)
	}
}

// TestTheBriefingChangesNothing is the read only rule at the door rather than at the interface.
// TestTheViewCanOnlyRead holds what the view may call; this holds that the front door refuses to be
// anything but a read, so nothing on this page can be made to act by being asked to.
func TestTheBriefingChangesNothing(t *testing.T) {
	client := aSystem(t)
	handler, err := Handler(client)
	if err != nil {
		t.Fatalf("build the handler: %v", err)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(method, "/", nil))
			if recorder.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s / answered %d, want it refused", method, recorder.Code)
			}
		})
	}
}

// TestAJobThatLandedAPullRequestShowsItAndSaysNothingAboutItsChecks drives a job the whole way through
// a real control plane: declared, started, answered with an address, landed. The page then reads the
// address back, and says the checks are unread rather than inventing a state the system does not hold.
//
// Reading a forge back is https://github.com/atlantic-blue/quay-crew/issues/549. Until it lands, a red
// check is not a thing this system can say, and this test is the guard on it never appearing to.
func TestAJobThatLandedAPullRequestShowsItAndSaysNothingAboutItsChecks(t *testing.T) {
	client, system := systemDoingJobs(t, &model.FakeRunner{
		Reply: "Pushed the branch and opened " + thePullRequest,
	})
	declare(t, client, &quaycrewv1.CreateJobRequest{
		Project: projectOf(t, client), Title: "make the listing sort by the clock it shows",
		Brief: "sort it", Repository: theRepository, Mode: model.PermissionModeOnTheNetwork(),
	})
	landed := tickUntil(t, client, system, job.PhaseDone)

	body, status := get(t, client, "/")
	if status != http.StatusOK {
		t.Fatalf("the briefing answered %d", status)
	}
	if !strings.Contains(body, "make the listing sort by the clock it shows") {
		t.Fatalf("the briefing does not carry the job that landed:\n%s", body)
	}
	if !strings.Contains(body, thePullRequest) {
		t.Errorf("the briefing does not say where the work is, and the job says %q:\n%s",
			landed.GetPullRequest(), body)
	}
	if !strings.Contains(body, checksUnread) {
		t.Errorf("the briefing says nothing about the checks, so a red one reads as a landed job:\n%s", body)
	}
}

// TestAJobTheModelRefusedIsBlockedWithItsReason. A job that went nowhere is the second question, and
// the reason is the whole of what makes it actionable: without it the page says something is wrong and
// sends the operator to a terminal to find out what.
func TestAJobTheModelRefusedIsBlockedWithItsReason(t *testing.T) {
	client, system := systemDoingJobs(t, &model.FakeRunner{Err: errors.New("the model refused")})
	declare(t, client, &quaycrewv1.CreateJobRequest{
		Project: projectOf(t, client), Title: "read the electricity bill", Brief: "open it",
	})
	failed := tickUntil(t, client, system, job.PhaseFailed)

	body, status := get(t, client, "/")
	if status != http.StatusOK {
		t.Fatalf("the briefing answered %d", status)
	}
	if !strings.Contains(body, "read the electricity bill") {
		t.Fatalf("the briefing does not carry the job that failed:\n%s", body)
	}
	if !strings.Contains(body, failed.GetReason()) {
		t.Errorf("the briefing does not say why it failed, and the job says %q:\n%s", failed.GetReason(), body)
	}
	blocked := strings.Index(body, `id="blocked"`)
	produced := strings.Index(body, `id="produced"`)
	where := strings.Index(body, "read the electricity bill")
	if where < blocked || where > produced {
		t.Errorf("the job that failed is not in the blocked block:\n%s", body)
	}
}

// declare puts a job on the record the way the command line does.
func declare(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, req *quaycrewv1.CreateJobRequest) *quaycrewv1.Job {
	t.Helper()
	created, err := client.CreateJob(context.Background(), req)
	if err != nil {
		t.Fatalf("declare a job: %v", err)
	}
	return created.GetJob()
}

// tickUntil moves the system on until the job reaches a phase, and fails saying where it got to
// instead. Nothing runs the controller in these tests, so a tick here is what its timer does in a
// running system.
func tickUntil(t *testing.T, client quaycrewv1.ControlPlaneServiceClient,
	system *controlplane.Server, phase string) *quaycrewv1.Job {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	var last *quaycrewv1.Job
	for time.Now().Before(deadline) {
		system.TickJob(ctx)
		listed, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{})
		if err != nil {
			t.Fatalf("list jobs: %v", err)
		}
		if len(listed.GetJobs()) == 0 {
			t.Fatal("the system holds no job at all")
		}
		last = listed.GetJobs()[0]
		if last.GetPhase() == phase {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the job is %q saying %q, want %q", last.GetPhase(), last.GetReason(), phase)
	return nil
}
