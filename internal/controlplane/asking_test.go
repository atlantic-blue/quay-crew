package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A session that has to decide something no measurement settles can ask, and the job stops there.
// These drive the whole of it through the control plane's own interface: the session asks, the task
// ends, the operator answers, and the system sends the answer back into the same conversation.

// heldOpen is a system whose model waits to be let go, so a test can act while a task is under way.
type heldOpen struct {
	server *controlplane.Server
	runner *model.FakeRunner
	kept   store.Store
	job    *quaycrewv1.Job
}

// aJobUnderWay declares a job, starts it, and answers once its task is running.
func aJobUnderWay(t *testing.T) heldOpen {
	t.Helper()
	runner := &model.FakeRunner{
		Reply: "i asked a question and stopped", Gate: make(chan struct{}), Started: make(chan struct{}),
	}
	kept := store.NewMemory()
	server := controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	declared := declaredIn(t, server, "choose where the transcripts are stored")
	server.TickJob(context.Background())
	<-runner.Started
	return heldOpen{server: server, runner: runner, kept: kept, job: declared}
}

// asking is the job as the system holds it now.
func (h heldOpen) reading(t *testing.T) *quaycrewv1.Job {
	t.Helper()
	found, err := h.server.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: h.job.GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return found.GetJob()
}

// theQuestion is what the acceptance run needed and could not ask: the resources about to be
// created, and what each costs while nothing is happening.
const theQuestion = "The store for the transcripts. Aurora Serverless version two bills a minimum " +
	"capacity continuously, about 43 dollars a month at rest. DynamoDB on demand bills nothing at " +
	"rest. Which do you want?"

// The whole of it, end to end: the session asks, the operator answers, and the answer arrives as
// the session's next task. Stopping at "the job is asking" would prove half a feature.
func TestAQuestionStopsTheJobAndTheAnswerReachesTheSessionThatAsked(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()

	asked, err := system.server.AskJob(asJobCredential(ctx, system.job.GetId()),
		&quaycrewv1.AskJobRequest{Question: theQuestion})
	if err != nil {
		t.Fatalf("AskJob: %v", err)
	}
	if asked.GetJob().GetPhase() != job.PhaseAsking {
		t.Fatalf("a job that asked is %q, want asking", asked.GetJob().GetPhase())
	}
	if asked.GetJob().GetQuestion() != theQuestion {
		t.Fatalf("the question reads back as %q", asked.GetJob().GetQuestion())
	}

	// The task ends, the way a session that has asked ends its turn. The controller must not read
	// that as the job being over: a job waiting on a person has not answered anything.
	close(system.runner.Gate)
	waitFor(t, func() bool { return len(tasksOf(t, system.server, system.job.GetId())) == 1 })
	system.server.TickJob(ctx)
	if phase := system.reading(t).GetPhase(); phase != job.PhaseAsking {
		t.Fatalf("the job moved to %q while it was waiting to be told something", phase)
	}

	answered, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "DynamoDB on demand. Nothing bills while nobody uses it.",
	})
	if err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	if answered.GetJob().GetPhase() != job.PhasePending {
		t.Fatalf("an answered job is %q, want pending so a controller starts it again", answered.GetJob().GetPhase())
	}

	// And what the session is actually handed, which is the half a test that stops at the row never
	// sees. It is read off the task record rather than off the double, because the record is what
	// the system really sent.
	system.server.TickJob(ctx)
	var sent []*quaycrewv1.Task
	waitFor(t, func() bool {
		sent = tasksOf(t, system.server, system.job.GetId())
		return len(sent) == 2
	})
	carried := sent[1].GetPrompt()
	if !strings.Contains(carried, "DynamoDB on demand") {
		t.Fatalf("the second task does not carry the answer:\n%s", carried)
	}
	if !strings.Contains(carried, theQuestion) {
		t.Fatalf("the second task does not restate the question, so the answer arrives at nothing:\n%s", carried)
	}
	if strings.Contains(carried, system.job.GetBrief()) {
		t.Fatalf("the second task sends the brief again, which asks the session to do the job over:\n%s", carried)
	}
}

// The record says a question was put and answered, so somebody reading the run tomorrow can see the
// decision without opening a container that is long gone.
func TestAskingAndBeingToldAreBothOnTheRecord(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()

	if _, err := system.server.AskJob(asJobCredential(ctx, system.job.GetId()),
		&quaycrewv1.AskJobRequest{Question: theQuestion}); err != nil {
		t.Fatalf("AskJob: %v", err)
	}
	if _, err := system.server.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "DynamoDB on demand",
	}); err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}

	listed, err := system.kept.ListJobEvents(ctx, system.job.GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	var asked, told *job.Event
	for _, one := range listed {
		switch one.Kind {
		case job.EventAsked:
			asked = one
		case job.EventTold:
			told = one
		}
	}
	if asked == nil || told == nil {
		t.Fatalf("the record does not carry both the question and the answer: %v", kindsOf(listed))
	}
	if !strings.Contains(asked.Detail, "Aurora Serverless") {
		t.Fatalf("the record of the question does not say what was asked: %q", asked.Detail)
	}
	if !strings.Contains(told.Detail, "DynamoDB") {
		t.Fatalf("the record of the answer does not say what was decided: %q", told.Detail)
	}
}

// A session asks about the job it is running and no other. The identifier in the request is checked
// against the credential rather than trusted, because a caller that could name any job could stop
// any job.
func TestASessionCannotAskAboutSomebodyElsesJob(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()

	_, err := system.server.AskJob(asJobCredential(ctx, system.job.GetId()),
		&quaycrewv1.AskJobRequest{Question: "which store?", Id: "0123456789abcdef01234567"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("naming another job was accepted: %v", err)
	}
	if phase := system.reading(t).GetPhase(); phase != job.PhaseRunning {
		t.Fatalf("the refused question moved the job to %q", phase)
	}
}

// A caller running no job has nobody waiting on its answer, so there is nothing for it to ask about.
// An operator wanting to ask a session something dispatches a task, which is the other direction.
func TestACallerRunningNoJobCannotAsk(t *testing.T) {
	system := aJobUnderWay(t)

	_, err := system.server.AskJob(context.Background(), &quaycrewv1.AskJobRequest{Question: "which store?"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a caller running no job asked a question: %v", err)
	}
}

// An empty question is refused rather than written. A job stopped on a question nobody can read is
// a job stopped for nothing, and the person answering has only what is written here.
func TestAQuestionWithNoWordsIsRefused(t *testing.T) {
	system := aJobUnderWay(t)

	_, err := system.server.AskJob(asJobCredential(context.Background(), system.job.GetId()),
		&quaycrewv1.AskJobRequest{Question: "   "})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a question with no words was accepted: %v", err)
	}
	if phase := system.reading(t).GetPhase(); phase != job.PhaseRunning {
		t.Fatalf("the refused question moved the job to %q", phase)
	}
}

// An answer to a job that asked nothing would start it again with a prompt nobody expects.
func TestAnsweringAJobThatAskedNothingIsRefused(t *testing.T) {
	system := aJobUnderWay(t)

	_, err := system.server.AnswerJob(context.Background(), &quaycrewv1.AnswerJobRequest{
		Id: system.job.GetId(), Answer: "DynamoDB on demand",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a job that asked nothing took an answer: %v", err)
	}
	if !strings.Contains(status.Convert(err).Message(), "asking") {
		t.Fatalf("the refusal does not say how to find the jobs that are waiting: %v", err)
	}
}

// Asking needs no verb, so no role can leave a session with guessing as its only move. Answering is
// mapped to no call at all, so nothing a role grants lets a session answer the question a person was
// asked.
func TestASessionMayAskWhateverItsRoleGrantsAndMayNeverAnswer(t *testing.T) {
	held := auth.Grant{Job: "job-1", Verbs: []string{role.VerbJobRead}}
	if err := controlplane.DeniedToJob(
		quaycrewv1.ControlPlaneService_AskJob_FullMethodName, &quaycrewv1.AskJobRequest{}, held); err != nil {
		t.Fatalf("a session running a job was refused its own question: %v", err)
	}
	everything := auth.Grant{Job: "job-1", Verbs: role.Grantable}
	if err := controlplane.DeniedToJob(
		quaycrewv1.ControlPlaneService_AnswerJob_FullMethodName, &quaycrewv1.AnswerJobRequest{}, everything); err == nil {
		t.Fatal("a session holding every verb answered a question a person was asked")
	}
}

// tasksOf is what the system sent the session doing this job, in the order it sent them.
func tasksOf(t *testing.T, server *controlplane.Server, id string) []*quaycrewv1.Task {
	t.Helper()
	found, err := server.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: id})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if found.GetJob().GetSession() == "" {
		return nil
	}
	listed, err := server.ListTasks(context.Background(),
		&quaycrewv1.ListTasksRequest{Session: found.GetJob().GetSession()})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	return listed.GetTasks()
}

func kindsOf(events []*job.Event) []string {
	var kinds []string
	for _, one := range events {
		kinds = append(kinds, one.Kind)
	}
	return kinds
}
