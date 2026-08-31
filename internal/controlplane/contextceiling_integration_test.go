//go:build integration

package controlplane_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/store"
)

// A session driven past its workspace's context ceiling hands the rest of its job over, and the work
// carries on in another session, over the whole path: a real Postgres, the real control plane, the
// real controller and the real gRPC interface, with nothing stood in for but the model.
//
// The table tests in internal/job prove the decision and the text. What they cannot answer is whether
// the record survives the movement: the handoff, the steps and the identity of the job all live in
// rows, and a store that lost any of them would leave the fresh session starting from nothing while
// every unit test stayed green.
func TestAJobPastTheContextCeilingCarriesOnInAnotherSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	dir := t.TempDir()
	durable := aRealStore(t, ctx)
	server := controlplane.NewServer(controlplane.Config{
		Store: durable,
		// An answer that does the work and names no pull request, which is the moment the system used
		// to send one more task into the fullest window the job ever had.
		Runner:   &model.FakeRunner{Reply: "I moved the picks query onto the new index and the tests pass"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Storage: sandbox.Storage{Dir: dir, Host: dir},
	})
	client := servedOver(t, server)

	workspace, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	declared, err := client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project.GetProject().GetId(), Title: "sort the listing",
		Brief:      "make the listing sort by the clock it shows",
		Repository: "atlantic-blue/quay-crew",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()

	// The first attempt: it starts, records a step, and answers.
	server.TickJob(ctx)
	waitForTasks(t, ctx, server)
	if _, err := server.RecordJobStep(asJobCredential(ctx, id),
		&quaycrewv1.RecordJobStepRequest{Summary: "read the issue"}); err != nil {
		t.Fatalf("RecordJobStep: %v", err)
	}

	// The window, filled. The size is what the model runtime last told a session in this workspace,
	// and what is used is what the last answer carried, both read off the files the system reads.
	full := readBack(t, ctx, client, id)
	theWindowHolds(t, dir, full.GetWorkspace(), 1_000_000)
	firstSession := full.GetSession()
	if firstSession == "" {
		t.Fatal("the job is in no session, so there is nothing to fill")
	}
	theModelCarried(t, dir, full.GetWorkspace(), theConversationOf(t, ctx, client, firstSession), 820_000)

	// The tick that used to send one more task into that conversation.
	server.TickJob(ctx)
	waitForTasks(t, ctx, server)
	asked := lastPromptIn(t, ctx, client, firstSession)
	if !job.AskingForAHandoff(asked) {
		t.Fatalf("the session over the ceiling was asked %q, want the handoff", asked)
	}
	if !strings.Contains(asked, strconv.Itoa(job.DefaultContextCeiling)+" per cent") {
		t.Fatalf("the ask does not name the ceiling it is enforcing:\n%s", asked)
	}

	// What that session leaves behind, written over the credential the system minted for its job.
	if _, err := server.RecordJobHandoff(asJobCredential(ctx, id), &quaycrewv1.RecordJobHandoffRequest{
		Left:  "the index is written, the query still reads the old one: branch 539-feat-index",
		Tried: "adding the index inside the renaming migration, which deadlocks",
	}); err != nil {
		t.Fatalf("RecordJobHandoff: %v", err)
	}

	server.TickJob(ctx)
	waitForTasks(t, ctx, server)

	carriedOn := readBack(t, ctx, client, id)
	if carriedOn.GetSession() == "" || carriedOn.GetSession() == firstSession {
		t.Fatalf("the rest of the job is in session %q, want a conversation the first one was not in (%q)",
			carriedOn.GetSession(), firstSession)
	}
	// The same job. Nothing restarted: the identity, the steps and the handoff are all the ones the
	// first session was working on, read back out of Postgres.
	if carriedOn.GetId() != id {
		t.Fatalf("the work carried on as job %s, want %s", carriedOn.GetId(), id)
	}
	if len(carriedOn.GetSteps()) != 1 {
		t.Fatalf("the job records %d steps, want the one the first session finished",
			len(carriedOn.GetSteps()))
	}
	if len(carriedOn.GetHandoffs()) != 1 {
		t.Fatalf("the job carries %d handoffs, want the one that was written", len(carriedOn.GetHandoffs()))
	}
	if handed := carriedOn.GetHandoffs()[0]; handed.GetSession() != firstSession {
		t.Fatalf("the handoff names conversation %q, want the one that wrote it, %q",
			handed.GetSession(), firstSession)
	}

	// And it carries something. A test that a second session starts passes whether or not the handoff
	// has anything in it, so this reads the task that session was actually given.
	handedOver := lastPromptIn(t, ctx, client, carriedOn.GetSession())
	for _, want := range []string{
		"the query still reads the old one",
		"539-feat-index",
		"which deadlocks",
		"read the issue",
		"make the listing sort by the clock it shows",
	} {
		if !strings.Contains(handedOver, want) {
			t.Fatalf("the fresh session is not told %q:\n%s", want, handedOver)
		}
	}

	// The record of why the conversation changed, in the rows rather than in a log line.
	kinds := jobEventKinds(t, ctx, durable, id)
	for _, want := range []string{job.EventHandedOver, job.EventHandedOn} {
		if !holdsKind(kinds, want) {
			t.Fatalf("the job's record reads %v, want it to carry %q", kinds, want)
		}
	}
}

// A session that will not write a handoff stops the job rather than having a fresh one started from
// nothing. Proved over the same path, because this is the case that decides whether the gate helps: a
// job carried on from an empty record pays for every discovery the first session made.
func TestAJobWhoseSessionWritesNoHandoffStopsRatherThanStartingAgain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	dir := t.TempDir()
	server := controlplane.NewServer(controlplane.Config{
		Store:    aRealStore(t, ctx),
		Runner:   &model.FakeRunner{Reply: "I moved the picks query onto the new index and the tests pass"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Storage: sandbox.Storage{Dir: dir, Host: dir},
	})
	client := servedOver(t, server)

	workspace, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	declared, err := client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project.GetProject().GetId(), Title: "sort the listing",
		Brief:      "make the listing sort by the clock it shows",
		Repository: "atlantic-blue/quay-crew",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()

	server.TickJob(ctx)
	waitForTasks(t, ctx, server)
	full := readBack(t, ctx, client, id)
	theWindowHolds(t, dir, full.GetWorkspace(), 1_000_000)
	firstSession := full.GetSession()
	theModelCarried(t, dir, full.GetWorkspace(), theConversationOf(t, ctx, client, firstSession), 820_000)

	// Asked for the handoff, and it answers without writing one.
	server.TickJob(ctx)
	waitForTasks(t, ctx, server)
	server.TickJob(ctx)
	waitForTasks(t, ctx, server)

	stopped := readBack(t, ctx, client, id)
	if stopped.GetPhase() != job.PhaseStopped {
		t.Fatalf("the job is %q saying %q, want stopped", stopped.GetPhase(), stopped.GetReason())
	}
	for _, want := range []string{"context ceiling", "nothing for a fresh session to start from"} {
		if !strings.Contains(stopped.GetReason(), want) {
			t.Fatalf("the reason says %q, want it to say %q", stopped.GetReason(), want)
		}
	}
	if stopped.GetSession() != firstSession {
		t.Fatalf("the job left session %q, and there was nothing to carry into another one", firstSession)
	}
}

// theWindowHolds writes down what the model runtime told a session in this workspace, which is the
// only way the system learns how big a context window is.
func theWindowHolds(t *testing.T, dir, workspace string, size int) {
	t.Helper()
	at := filepath.Join(dir, "workspaces", workspace, "claude")
	if err := os.MkdirAll(at, 0o777); err != nil {
		t.Fatalf("make the conversation directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(at, sandbox.ContextWindowFile),
		[]byte(strconv.Itoa(size)+"\n"), 0o666); err != nil {
		t.Fatalf("write the window size: %v", err)
	}
}

// theModelCarried writes the transcript the model keeps, which is where the system reads what the
// last answer carried. It is what is in the window rather than what the conversation cost: cost only
// grows, and the window empties again when the model compacts.
func theModelCarried(t *testing.T, dir, workspace, conversation string, carried int) {
	t.Helper()
	if conversation == "" {
		t.Fatal("the session holds no conversation, so there is no transcript to write")
	}
	at := filepath.Join(dir, "workspaces", workspace, "claude", "projects", "-home-agent-workspace")
	if err := os.MkdirAll(at, 0o777); err != nil {
		t.Fatalf("make the transcript directory: %v", err)
	}
	line := fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","usage":`+
		`{"input_tokens":0,"output_tokens":400,"cache_read_input_tokens":%d,`+
		`"cache_creation_input_tokens":0}}}`+"\n", carried)
	if err := os.WriteFile(filepath.Join(at, conversation+sandbox.ConversationFile),
		[]byte(line), 0o666); err != nil {
		t.Fatalf("write the transcript: %v", err)
	}
}

func readBack(t *testing.T, ctx context.Context, client quaycrewv1.ControlPlaneServiceClient,
	id string) *quaycrewv1.Job {
	t.Helper()
	found, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return found.GetJob()
}

// lastPromptIn is the text of the last task a session was given, which is what the system actually
// asked of it rather than what a caller believes it asked.
func lastPromptIn(t *testing.T, ctx context.Context, client quaycrewv1.ControlPlaneServiceClient,
	session string) string {
	t.Helper()
	listed, err := client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	tasks := listed.GetTasks()
	if len(tasks) == 0 {
		t.Fatalf("session %s was asked nothing at all", session)
	}
	return tasks[len(tasks)-1].GetPrompt()
}

// jobEventKinds is what happened to a job, read out of the store the movements were written to. The
// records land in the same transaction as the row they describe, so this is the evidence that the
// conversation changed for the reason the system says it did.
func jobEventKinds(t *testing.T, ctx context.Context, durable *store.Postgres, id string) []string {
	t.Helper()
	listed, err := durable.ListJobEvents(ctx, id)
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	kinds := make([]string, 0, len(listed))
	for _, one := range listed {
		kinds = append(kinds, one.Kind)
	}
	return kinds
}

// theConversationOf is the handle the model keeps a session's transcript under, which the system
// wrote onto the session row when its task ran.
func theConversationOf(t *testing.T, ctx context.Context, client quaycrewv1.ControlPlaneServiceClient,
	session string) string {
	t.Helper()
	found, err := client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: session})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	return found.GetSession().GetModelSessionId()
}

func holdsKind(kinds []string, want string) bool {
	for _, one := range kinds {
		if one == want {
			return true
		}
	}
	return false
}

// waitForTasks blocks until every detached task the system started has landed, so a tick reads a task
// that has finished rather than one that is still going.
func waitForTasks(t *testing.T, ctx context.Context, server *controlplane.Server) {
	t.Helper()
	waiting, done := context.WithTimeout(ctx, 60*time.Second)
	defer done()
	server.WaitForTasks(waiting)
	if waiting.Err() != nil {
		t.Fatal("a detached task never landed")
	}
}
