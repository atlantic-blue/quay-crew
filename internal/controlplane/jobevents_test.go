package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/protobuf/proto"
)

// refusingLog takes nothing. A broker that is down is not a reason to lose job, so this is the
// double the rule "an export never fails a task" is proved against.
type refusingLog struct{ tried int }

func (r *refusingLog) Publish(context.Context, string, []byte, []byte) error {
	r.tried++
	return errAtTheBroker
}
func (r *refusingLog) Consume(context.Context, string, []string, messaging.Handler) error { return nil }
func (r *refusingLog) ConsumePattern(context.Context, string, string, messaging.Handler) error {
	return nil
}
func (r *refusingLog) Close() {}

var errAtTheBroker = errBroker{}

type errBroker struct{}

func (errBroker) Error() string { return "the broker refused this record" }

// crewWithLog is a crew whose event log is the one given.
func crewWithLog(t *testing.T, log messaging.EventLog) (*controlplane.Server, store.Store) {
	t.Helper()
	kept := store.NewMemory()
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: &model.FakeRunner{Reply: "done"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(), Events: log,
	}), kept
}

// declaredIn is a job in a fresh workspace and project.
func declaredIn(t *testing.T, server *controlplane.Server, title string) *quaycrewv1.Job {
	t.Helper()
	_, project := newProject(t, server)
	declared, err := server.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: title, Brief: "open the bill and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return declared.GetJob()
}

// The record goes on the log after the transaction, keyed by the job so one job's records
// stay in order on one partition.
func TestDeclaringJobPutsARecordOnTheJobStream(t *testing.T) {
	log := messaging.NewMemory()
	server, _ := crewWithLog(t, log)
	declared := declaredIn(t, server, "read the electricity bill")

	records := log.RecordsOn("acme.job")
	if len(records) != 1 {
		t.Fatalf("%d records landed on the job stream, want the declaration", len(records))
	}
	if string(records[0].Key) != declared.GetId() {
		t.Fatalf("the record is keyed by %q, and one job's records have to share a partition",
			string(records[0].Key))
	}

	var event quaycrewv1.JobEvent
	if err := proto.Unmarshal(records[0].Value, &event); err != nil {
		t.Fatalf("the record does not decode as a job event: %v", err)
	}
	if event.GetKind() != job.EventDeclared {
		t.Fatalf("the record says %q happened", event.GetKind())
	}
	if event.GetJob() != declared.GetId() || event.GetWorkspace() != declared.GetWorkspace() {
		t.Fatalf("the record names job %q in workspace %q", event.GetJob(), event.GetWorkspace())
	}
	if event.GetDetail() != "read the electricity bill" {
		t.Fatalf("the record says %q", event.GetDetail())
	}
	if event.GetTraceId() == "" {
		t.Fatal("the record carries no trace, so nothing joins it to the span or the log line")
	}
	if event.GetOccurredAt() == nil {
		t.Fatal("the record does not say when it happened")
	}
}

// The rule the whole export hangs off. The store is the truth and the log is a copy, so a broker
// that refuses everything costs the copy and nothing else.
func TestAnExportThatFailsDoesNotFailTheJob(t *testing.T) {
	log := &refusingLog{}
	server, kept := crewWithLog(t, log)

	declared := declaredIn(t, server, "read the electricity bill")
	if log.tried == 0 {
		t.Fatal("nothing was offered to the broker, so this proves nothing about a broker that refuses")
	}

	// The row is there, whole, with its record of how it came to exist.
	found, err := kept.GetJob(context.Background(), declared.GetId())
	if err != nil {
		t.Fatalf("the job is not in the store after an export that failed: %v", err)
	}
	if found.Phase != job.PhasePending {
		t.Fatalf("the job reads %q", found.Phase)
	}
	events, err := kept.ListJobEvents(context.Background(), declared.GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != job.EventDeclared {
		t.Fatalf("%d records are in the store after an export that failed", len(events))
	}

	// And the crew carries on serving, which is the other half of "never fails a command".
	if _, err := server.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{}); err != nil {
		t.Fatalf("a crew whose broker refuses stopped answering: %v", err)
	}
}

// A crew with no broker configured is the default. Nothing is exported and the whole record is kept.
func TestWithNoBrokerNothingIsExportedAndTheRecordIsWhole(t *testing.T) {
	server, kept := crewWithLog(t, nil)
	declared := declaredIn(t, server, "read the electricity bill")

	events, err := kept.ListJobEvents(context.Background(), declared.GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("%d records are in the store on a crew with no broker", len(events))
	}
	if events[0].TraceID == "" {
		t.Fatal("the record carries no trace on a crew with no broker")
	}
}

// Every detail goes through the crew's redactor before it is written or exported. What a caller
// types can be a credential, and everything here is persisted.
func TestASecretInADetailReachesNeitherTheRecordNorTheLog(t *testing.T) {
	const secret = "sk-ant-the-value-nobody-should-store"
	log := messaging.NewMemory()
	server, kept := crewWithLog(t, log)
	workspace, project := newProject(t, server)

	if _, err := server.SetSecret(context.Background(), &quaycrewv1.SetSecretRequest{
		Workspace: workspace, Key: "ANTHROPIC_API_KEY", Value: secret,
	}); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}

	declared, err := server.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "deploy with " + secret, Brief: "open the bill",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	events, err := kept.ListJobEvents(context.Background(), declared.GetJob().GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("%d records came back", len(events))
	}
	if strings.Contains(events[0].Detail, secret) {
		t.Fatalf("the stored record carries the secret: %q", events[0].Detail)
	}
	if !strings.Contains(events[0].Detail, "ANTHROPIC_API_KEY") {
		t.Fatalf("the stored record does not name what was taken out: %q", events[0].Detail)
	}

	records := log.RecordsOn("acme.job")
	if len(records) != 1 {
		t.Fatalf("%d records landed on the log", len(records))
	}
	if strings.Contains(string(records[0].Value), secret) {
		t.Fatal("the exported record carries the secret")
	}
}

// A movement of a job that was stopped is a record too, and it is the one a reader comes looking for.
func TestStoppingJobPutsItsReasonOnTheRecordAndTheLog(t *testing.T) {
	log := messaging.NewMemory()
	server, kept := crewWithLog(t, log)
	declared := declaredIn(t, server, "read the electricity bill")

	if _, err := server.StopJob(context.Background(), &quaycrewv1.StopJobRequest{
		Id: declared.GetId(), Reason: "the bill arrived by post",
	}); err != nil {
		t.Fatalf("StopJob: %v", err)
	}

	events, err := kept.ListJobEvents(context.Background(), declared.GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	if len(events) != 2 || events[1].Kind != job.EventStopped {
		t.Fatalf("the history is %d records ending in %q", len(events), events[len(events)-1].Kind)
	}
	if events[1].TraceID != events[0].TraceID {
		t.Fatalf("the two records trace %q and %q, and one job is one trace",
			events[0].TraceID, events[1].TraceID)
	}
	if len(log.RecordsOn("acme.job")) != 2 {
		t.Fatalf("%d records landed on the log, want the declaration and the stop",
			len(log.RecordsOn("acme.job")))
	}
}

// One identifier joins the tree. A job carries the trace of the call that declared it, so
// a reader holding the row can open the trace and every line written under it.
func TestJobCarriesTheTraceOfTheCallThatDeclaredIt(t *testing.T) {
	server, _ := crewWithLog(t, nil)
	declared := declaredIn(t, server, "read the electricity bill")
	if len(declared.GetTraceId()) != 32 {
		t.Fatalf("the job traces %q, which joins to nothing", declared.GetTraceId())
	}
}
