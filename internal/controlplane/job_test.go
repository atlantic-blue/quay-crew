package controlplane_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Job is declared intent. Nothing runs it in this slice, so what is proved here is the record, the
// refusals, and that a caller can read back what it declared.

// declareJob writes one plain job into a project.
func declareJob(t *testing.T, s *controlplane.Server, project, title string) *quaycrewv1.Job {
	t.Helper()
	created, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: title, Brief: "open the bill and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return created.GetJob()
}

// refusalOf declares job that should be refused and hands back the refusal.
func refusalOf(t *testing.T, s *controlplane.Server, req *quaycrewv1.CreateJobRequest) error {
	t.Helper()
	created, err := s.CreateJob(context.Background(), req)
	if err == nil {
		t.Fatalf("the declaration was accepted as %s, and it should have been refused", created.GetJob().GetId())
	}
	return err
}

// A job opens pending, under nothing, with the system's own identifier on it.
func TestDeclaredJobOpensPendingUnderNothing(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	declared := declareJob(t, s, project, "read the electricity bill")

	if declared.GetPhase() != job.PhasePending {
		t.Fatalf("the job opens %q, want pending", declared.GetPhase())
	}
	if len(declared.GetId()) != 24 {
		t.Fatalf("the identifier is %q, want the 24 characters the system mints", declared.GetId())
	}
	if declared.GetCause() != "" || declared.GetRun() != "" {
		t.Fatalf("the job says %q caused it and it is a step of run %q, want neither",
			declared.GetCause(), declared.GetRun())
	}
	if declared.GetVersion() != 1 || declared.GetObservedVersion() != 0 {
		t.Fatalf("the job is version %d observed at %d, want 1 and 0", declared.GetVersion(), declared.GetObservedVersion())
	}
	if declared.GetCreatedAt() == nil || declared.GetUpdatedAt() == nil {
		t.Fatal("the job does not carry when it was declared")
	}
	if declared.GetSession() != "" || declared.GetAnswer() != "" || declared.GetAttempts() != 0 {
		t.Fatal("job nothing has run carries a session, an answer or an attempt")
	}
}

// The whole of what this slice buys: the intent is a row, so it is still there when whoever asked
// for it has gone.
func TestIntentOutlivesTheCallerThatDeclaredIt(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	declaring, giveUp := context.WithCancel(context.Background())
	created, err := s.CreateJob(declaring, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open the bill and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	giveUp()

	found, err := s.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: created.GetJob().GetId()})
	if err != nil {
		t.Fatalf("the job was gone once the caller was: %v", err)
	}
	if found.GetJob().GetBrief() != "open the bill and say when it is due" {
		t.Fatalf("the brief reads back as %q", found.GetJob().GetBrief())
	}
}

func TestAnIdentifierTheCallerChoseIsRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	err := refusalOf(t, s, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the bill", Brief: "open it", Id: "0123456789abcdef01234567",
	})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("the refusal is %v, want InvalidArgument", status.Code(err))
	}
	if !strings.Contains(err.Error(), "assigns the identifier") {
		t.Fatalf("the refusal says %q, want it to say the system assigns the identifier", err)
	}
}

// The way off the old interface. A caller that used to put a job under another one gets a refusal
// that names what to type instead, on the tool, and the field it used to send is gone from the wire:
// a request carrying it is refused before anything is written.
func TestTheParentFlagIsRefusedNamingWhatToTypeInstead(t *testing.T) {
	fields := (&quaycrewv1.CreateJobRequest{}).ProtoReflect().Descriptor().Fields()
	for at := 0; at < fields.Len(); at++ {
		if name := string(fields.Get(at).Name()); name == "parent" || name == "depth" {
			t.Fatalf("a request still carries %q, so a caller can still ask for a job under a job", name)
		}
	}
}

func TestTheDeclarationIsCheckedBeforeAnythingIsWritten(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	_ = refusalOf(t, s, &quaycrewv1.CreateJobRequest{Project: project, Title: "", Brief: "open it"})

	listed, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	if len(listed.GetJobs()) != 0 {
		t.Fatalf("a refused declaration left %d rows behind", len(listed.GetJobs()))
	}
}

// Every rule of the declaration reaches the caller as a sentence saying what to do instead.
func TestEveryRuleOfADeclarationIsRefusedWithASentence(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	for _, tc := range []struct {
		name    string
		request *quaycrewv1.CreateJobRequest
		says    []string
	}{
		{
			name:    "no title",
			request: &quaycrewv1.CreateJobRequest{Project: project, Brief: "open it"},
			says:    []string{"title"},
		},
		{
			name: "a title over the ceiling",
			request: &quaycrewv1.CreateJobRequest{
				Project: project, Title: strings.Repeat("t", job.TitleLimit+1), Brief: "open it"},
			says: []string{"201", "200"},
		},
		{
			name:    "no brief",
			request: &quaycrewv1.CreateJobRequest{Project: project, Title: "read the bill"},
			says:    []string{"brief"},
		},
		{
			name: "a brief over the ceiling",
			request: &quaycrewv1.CreateJobRequest{
				Project: project, Title: "read the bill", Brief: strings.Repeat("b", job.BriefLimit+1)},
			says: []string{"16385", "16384"},
		},
		{
			name: "a mode that is not a mode",
			request: &quaycrewv1.CreateJobRequest{
				Project: project, Title: "read the bill", Brief: "open it", Mode: "yolo"},
			says: []string{"plan", "edits", "dangerous"},
		},
		{
			name: "an expected file outside the working directory",
			request: &quaycrewv1.CreateJobRequest{
				Project: project, Title: "read the bill", Brief: "open it", ExpectFile: "/etc/passwd"},
			says: []string{"/etc/passwd", "working directory"},
		},
		{
			name: "an expected file that climbs out",
			request: &quaycrewv1.CreateJobRequest{
				Project: project, Title: "read the bill", Brief: "open it", ExpectFile: "../secrets.txt"},
			says: []string{"climbs out"},
		},
		{
			name: "a brief that asks the job to wait",
			request: &quaycrewv1.CreateJobRequest{
				Project: project, Title: "read the bill",
				Brief: "push it, watch the checks and merge on green"},
			says: []string{"watch the checks", "cannot wait", "krewe flow import"},
		},
		{
			name: "a budget below zero",
			request: &quaycrewv1.CreateJobRequest{
				Project: project, Title: "read the bill", Brief: "open it", BudgetTokens: -1},
			says: []string{"budget"},
		},
		{
			name: "more labels than the ceiling",
			request: &quaycrewv1.CreateJobRequest{
				Project: project, Title: "read the bill", Brief: "open it", Labels: seventeenLabels()},
			says: []string{"17", "16"},
		},
		{
			name: "a label value over the ceiling",
			request: &quaycrewv1.CreateJobRequest{
				Project: project, Title: "read the bill", Brief: "open it",
				Labels: map[string]string{"owner": strings.Repeat("v", job.LabelLimit+1)}},
			says: []string{"64", "63"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := refusalOf(t, s, tc.request)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("the refusal is %v, want InvalidArgument", status.Code(err))
			}
			for _, want := range tc.says {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal says %q, want it to say %q", err, want)
				}
			}
		})
	}
}

// A role the workspace does not hold is refused by name, at the write, while somebody is looking.
func TestJobNamingARoleTheWorkspaceDoesNotHoldIsRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	err := refusalOf(t, s, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the bill", Brief: "open it", Role: "backlog-clearer",
	})

	if !strings.Contains(err.Error(), "backlog-clearer") {
		t.Fatalf("the refusal says %q, want it to name the role", err)
	}
	if !strings.Contains(err.Error(), "krewe role attach") {
		t.Fatalf("the refusal says %q, want it to say how to give the workspace the role", err)
	}
}

// The version is pinned at the write, so editing a role tomorrow cannot change job declared today.
func TestJobInARoleIsPinnedToTheVersionTheWorkspaceHolds(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	workspace, project := newProject(t, s)
	importRoleAt(t, s, "backlog-clearer", 1)
	attachRole(t, s, workspace, "backlog-clearer")

	declared, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "clear the backlog", Brief: "read the pull requests", Role: "backlog-clearer",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if declared.GetJob().GetRole() != "backlog-clearer" || declared.GetJob().GetRoleVersion() != 1 {
		t.Fatalf("the job runs as %q at version %d, want backlog-clearer at 1",
			declared.GetJob().GetRole(), declared.GetJob().GetRoleVersion())
	}

	// A newer version imported and attached afterwards leaves the job already declared alone.
	importRoleAt(t, s, "backlog-clearer", 2)
	attachRole(t, s, workspace, "backlog-clearer")

	found, err := s.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if found.GetJob().GetRoleVersion() != 1 {
		t.Fatalf("the job moved to version %d, and a pin that moves is not a pin", found.GetJob().GetRoleVersion())
	}
}

func TestJobWaitingForSomethingTheSystemDoesNotHoldIsRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	err := refusalOf(t, s, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the bill", Brief: "open it",
		After: []string{"0123456789abcdef01234567"},
	})

	if !strings.Contains(err.Error(), "0123456789abcdef01234567") {
		t.Fatalf("the refusal says %q, want it to name the identifier it cannot find", err)
	}
}

func TestJobWaitsForJobThatExists(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	first := declareJob(t, s, project, "read the electricity bill")
	second, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "pay the electricity bill", Brief: "pay it",
		After: []string{first.GetId()},
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if got := second.GetJob().GetAfter(); len(got) != 1 || got[0] != first.GetId() {
		t.Fatalf("the job waits for %v, want the first job", got)
	}
}

func TestJobInAProjectThatDoesNotExistIsRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})

	err := refusalOf(t, s, &quaycrewv1.CreateJobRequest{
		Project: "nowhere", Title: "read the bill", Brief: "open it",
	})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("the refusal is %v, want NotFound", status.Code(err))
	}
}

func TestJobWithNoProjectSaysWhereItGoes(t *testing.T) {
	s := newServer(&model.FakeRunner{})

	err := refusalOf(t, s, &quaycrewv1.CreateJobRequest{Title: "read the bill", Brief: "open it"})

	if !strings.Contains(err.Error(), "project") {
		t.Fatalf("the refusal says %q, want it to say job needs a project", err)
	}
}

func TestJobNobodyHoldsIsRefusedByName(t *testing.T) {
	s := newServer(&model.FakeRunner{})

	_, err := s.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: "0123456789abcdef01234567"})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("the refusal is %v, want NotFound", status.Code(err))
	}
	if !strings.Contains(err.Error(), "krewe job list") {
		t.Fatalf("the refusal says %q, want it to say where to look", err)
	}
}

// A listing is for finding job. The answers are left out, and one job read on its own
// carries its answer whole.
func TestAListingCarriesNoAnswersAndReadingOneJobDoes(t *testing.T) {
	s, kept := serverOnAStore(t)
	workspace, project := newProject(t, s)
	id := jobWithAnAnswer(t, kept, workspace, project, "read the electricity bill", "the bill is due on the 14th")

	listed, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	if len(listed.GetJobs()) != 1 {
		t.Fatalf("the listing holds %d jobs, want 1", len(listed.GetJobs()))
	}
	if listed.GetJobs()[0].GetAnswer() != "" {
		t.Fatalf("the listing carries an answer: %q", listed.GetJobs()[0].GetAnswer())
	}
	if listed.GetJobs()[0].GetTitle() != "read the electricity bill" {
		t.Fatalf("the listing carries no title: %q", listed.GetJobs()[0].GetTitle())
	}

	found, err := s.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: id})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if found.GetJob().GetAnswer() != "the bill is due on the 14th" {
		t.Fatalf("the answer reads back as %q", found.GetJob().GetAnswer())
	}
}

func TestAListingIsNewestFirstAndNarrowsByPhase(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	first := declareJob(t, s, project, "read the electricity bill")
	second := declareJob(t, s, project, "pay the electricity bill")

	listed, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	if len(listed.GetJobs()) != 2 || listed.GetJobs()[0].GetId() != second.GetId() {
		t.Fatalf("the listing opens with %q, want the newest first", listed.GetJobs()[0].GetTitle())
	}

	if _, err := s.StopJob(context.Background(), &quaycrewv1.StopJobRequest{
		Id: first.GetId(), Reason: "the bill is not due yet",
	}); err != nil {
		t.Fatalf("StopJob: %v", err)
	}
	pending, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{
		Project: project, Phase: job.PhasePending,
	})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	if len(pending.GetJobs()) != 1 || pending.GetJobs()[0].GetId() != second.GetId() {
		t.Fatalf("the pending job is %d rows, want the one that is still pending", len(pending.GetJobs()))
	}
}

func TestAListingNarrowedByAWordThatIsNotAPhaseIsRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	_, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project, Phase: "idle"})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("the refusal is %v, want InvalidArgument", status.Code(err))
	}
	for _, phase := range job.Phases() {
		if !strings.Contains(err.Error(), phase) {
			t.Errorf("the refusal says %q, want it to offer %q", err, phase)
		}
	}
}

func TestStoppingJobKeepsTheReasonAndStopsItOnce(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	declared := declareJob(t, s, project, "read the electricity bill")

	stopped, err := s.StopJob(context.Background(), &quaycrewv1.StopJobRequest{
		Id: declared.GetId(), Reason: "the bill is not due yet",
	})
	if err != nil {
		t.Fatalf("StopJob: %v", err)
	}
	if stopped.GetJob().GetPhase() != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", stopped.GetJob().GetPhase())
	}
	if stopped.GetJob().GetReason() != "the bill is not due yet" {
		t.Fatalf("the reason is %q", stopped.GetJob().GetReason())
	}
	if stopped.GetJob().GetFinishedAt() == nil {
		t.Fatal("stopped job does not carry when it finished")
	}

	_, err = s.StopJob(context.Background(), &quaycrewv1.StopJobRequest{
		Id: declared.GetId(), Reason: "changed my mind",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stopping job that already ended answered %v, want FailedPrecondition", status.Code(err))
	}
	found, _ := s.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: declared.GetId()})
	if found.GetJob().GetReason() != "the bill is not due yet" {
		t.Fatalf("the second stop overwrote the reason: %q", found.GetJob().GetReason())
	}
}

// A stop with no reason still says something, because a job that went quiet and a job somebody halted
// must never read the same.
func TestStoppingJobWithNoReasonSaysWhoStoppedIt(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	declared := declareJob(t, s, project, "read the electricity bill")

	stopped, err := s.StopJob(context.Background(), &quaycrewv1.StopJobRequest{Id: declared.GetId()})
	if err != nil {
		t.Fatalf("StopJob: %v", err)
	}
	if stopped.GetJob().GetReason() == "" {
		t.Fatal("job stopped with no reason carries no reason at all")
	}
}

func TestStoppingJobNobodyHoldsIsRefusedByName(t *testing.T) {
	s := newServer(&model.FakeRunner{})

	_, err := s.StopJob(context.Background(), &quaycrewv1.StopJobRequest{Id: "0123456789abcdef01234567"})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("the refusal is %v, want NotFound", status.Code(err))
	}
}

// The record of what happened is a row beside the row it describes, written in the same breath.
func TestDeclaringAndStoppingJobWriteTheirOwnRecords(t *testing.T) {
	s, kept := serverOnAStore(t)
	_, project := newProject(t, s)
	declared := declareJob(t, s, project, "read the electricity bill")

	events := jobEvents(t, kept, declared.GetId())
	if len(events) != 1 || events[0].Kind != job.EventDeclared {
		t.Fatalf("the records after a declaration are %v, want one job.declared", kinds(events))
	}
	if events[0].Detail != "read the electricity bill" {
		t.Fatalf("the record says %q, want it to name the title", events[0].Detail)
	}

	if _, err := s.StopJob(context.Background(), &quaycrewv1.StopJobRequest{
		Id: declared.GetId(), Reason: "the bill is not due yet",
	}); err != nil {
		t.Fatalf("StopJob: %v", err)
	}
	events = jobEvents(t, kept, declared.GetId())
	if len(events) != 2 || events[1].Kind != job.EventStopped {
		t.Fatalf("the records after a stop are %v, want job.declared then job.stopped", kinds(events))
	}
	if events[1].Detail != "the bill is not due yet" {
		t.Fatalf("the record says %q, want it to name the reason", events[1].Detail)
	}
}

// Everything the system persists goes through the redactor, and a caller can paste a credential into
// anything it types.
func TestWhatTheSystemRecordsIsRedacted(t *testing.T) {
	s, kept := serverOnAStore(t)
	workspace, project := newProject(t, s)
	if _, err := s.SetSecret(context.Background(), &quaycrewv1.SetSecretRequest{
		Workspace: workspace, Key: "HOUSE_PASSWORD", Value: "the-house-password-42",
	}); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	declared := declareJob(t, s, project, "read the electricity bill")

	if _, err := s.StopJob(context.Background(), &quaycrewv1.StopJobRequest{
		Id: declared.GetId(), Reason: "it printed the-house-password-42 into the log",
	}); err != nil {
		t.Fatalf("StopJob: %v", err)
	}

	for _, event := range jobEvents(t, kept, declared.GetId()) {
		if strings.Contains(event.Detail, "the-house-password-42") {
			t.Fatalf("the record carries a value the workspace sealed, in the clear: %q", event.Detail)
		}
	}
}

// A deadline and a set of labels are declared here and read by nothing yet. They still have to come
// back the way they went in, or the slice that reads them starts from a record that lost them.
func TestWhatIsDeclaredComesBackWholeEvenWhereNothingReadsItYet(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	created, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
		Mode: "plan", ExpectFile: "notes/bill.md", ExpectContains: "due",
		Deadline: timestamppb.New(deadline), BudgetTokens: 5000,
		Labels: map[string]string{"owner": "house"},
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	found, err := s.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: created.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	one := found.GetJob()
	if one.GetMode() != "plan" || one.GetExpectFile() != "notes/bill.md" || one.GetExpectContains() != "due" {
		t.Fatalf("the claim reads back as %q %q %q", one.GetMode(), one.GetExpectFile(), one.GetExpectContains())
	}
	if one.GetBudgetTokens() != 5000 {
		t.Fatalf("the budget reads back as %d", one.GetBudgetTokens())
	}
	if !one.GetDeadline().AsTime().Equal(deadline) {
		t.Fatalf("the deadline reads back as %v, want %v", one.GetDeadline().AsTime(), deadline)
	}
	if one.GetLabels()["owner"] != "house" {
		t.Fatalf("the labels read back as %v", one.GetLabels())
	}
}

// The mode is stored in the runtime's own spelling, because that is what a dispatch will send.
func TestTheModeIsStoredInTheSpellingTheRuntimeUses(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	created, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the bill", Brief: "open it", Mode: "edits",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if want := model.PermissionAcceptEdits; created.GetJob().GetMode() != want {
		t.Fatalf("the mode is stored as %q, want %q", created.GetJob().GetMode(), want)
	}
}

func seventeenLabels() map[string]string {
	labels := map[string]string{}
	for i := 0; i <= job.LabelCount; i++ {
		labels[fmt.Sprintf("key-%d", i)] = "value"
	}
	return labels
}

func kinds(events []*job.Event) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Kind)
	}
	return out
}

func importRoleAt(t *testing.T, s *controlplane.Server, name string, version int) {
	t.Helper()
	manifest := fmt.Sprintf("name: %s\nversion: %d\nsummary: clears the backlog\nmodel: opus\nreceives:\n  - job\n", name, version)
	if _, err := s.ImportRole(context.Background(), &quaycrewv1.ImportRoleRequest{
		Files: []*quaycrewv1.RoleFile{
			{Path: role.ManifestFile, Body: []byte(manifest)},
			{Path: role.BriefFile, Body: []byte("Read the open pull requests.")},
		},
	}); err != nil {
		t.Fatalf("ImportRole: %v", err)
	}
}

func attachRole(t *testing.T, s *controlplane.Server, workspace, name string) {
	t.Helper()
	if _, err := s.AttachRole(context.Background(), &quaycrewv1.AttachRoleRequest{
		Workspace: workspace, Name: name,
	}); err != nil {
		t.Fatalf("AttachRole: %v", err)
	}
}

// serverOnAStore hands back the store the server was built on.
//
// Two things this slice records have no call yet: the answer, which a controller writes, and the
// record of what happened, which is read by the export that is not built. Both are store level
// facts here, so a test asserts on them where they live.
func serverOnAStore(t *testing.T) (*controlplane.Server, store.Store) {
	t.Helper()
	kept := store.NewMemory()
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: &model.FakeRunner{}, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	}), kept
}

func jobEvents(t *testing.T, kept store.Store, id string) []*job.Event {
	t.Helper()
	events, err := kept.ListJobEvents(context.Background(), id)
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	return events
}

// jobWithAnAnswer puts job that already answered into the store, which is what a controller will
// write in the slice that runs job. The read path is proved against it now, because the read path
// is what this slice ships.
func jobWithAnAnswer(t *testing.T, kept store.Store, workspace, project, title, answer string) string {
	t.Helper()
	declared := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: title, Brief: "open it", Version: 1, Phase: job.PhaseDone, Answer: answer,
	}
	if err := kept.CreateJob(context.Background(), declared, &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: declared.ID,
		Workspace: workspace, Project: project, Detail: title, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return declared.ID
}

// importRoleReceiving imports a role that receives exactly what it is given, so a test can put the
// boundary where it needs it.
func importRoleReceiving(t *testing.T, s *controlplane.Server, name string, version int, material ...string) {
	t.Helper()
	receives := ""
	for _, one := range material {
		receives += fmt.Sprintf("  - %s\n", one)
	}
	manifest := fmt.Sprintf("name: %s\nversion: %d\nsummary: clears the backlog\nmodel: opus\nreceives:\n%s",
		name, version, receives)
	if _, err := s.ImportRole(context.Background(), &quaycrewv1.ImportRoleRequest{
		Files: []*quaycrewv1.RoleFile{
			{Path: role.ManifestFile, Body: []byte(manifest)},
			{Path: role.BriefFile, Body: []byte("Read the open pull requests.")},
		},
	}); err != nil {
		t.Fatalf("ImportRole: %v", err)
	}
}

// The boundary is checked while the caller is looking, as well as at the dispatch: a refusal that
// arrives hours later has nothing pointing back at the declaration.
func TestJobRequiringMaterialItsRoleDoesNotReceiveIsRefusedAtTheWrite(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	workspace, project := newProject(t, s)
	importRoleReceiving(t, s, "test-writer", 1, "job")
	attachRole(t, s, workspace, "test-writer")

	err := refusalOf(t, s, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "write the tests", Brief: "from the job alone",
		Role: "test-writer", Requires: []string{"context"},
	})

	for _, want := range []string{"test-writer", "context", "declare the job without"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal says %q, want it to name %q", err, want)
		}
	}
	listed, listErr := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project})
	if listErr != nil {
		t.Fatalf("ListJob: %v", listErr)
	}
	if len(listed.GetJobs()) != 0 {
		t.Fatalf("the system holds %d jobs, and a refusal must write no row", len(listed.GetJobs()))
	}
}

func TestJobRequiringWhatItsRoleReceivesIsKeptWholeAndReadBack(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	workspace, project := newProject(t, s)
	importRoleReceiving(t, s, "backlog-clearer", 1, "job", "context")
	attachRole(t, s, workspace, "backlog-clearer")

	declared, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "clear the backlog", Brief: "read the pull requests",
		Role: "backlog-clearer", Requires: []string{"context", "job"},
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	required := declared.GetJob().GetRequires()
	if len(required) != 2 || required[0] != "context" || required[1] != "job" {
		t.Fatalf("the job requires %v, want context and job", required)
	}
}

// Job that names no role requires its material of nobody, so nothing about it changes.
func TestJobWithNoRoleIsNeverHeldToAnyBoundary(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	declared, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
		Requires: []string{"context", "skills"},
	})
	if err != nil {
		t.Fatalf("job with no role was refused: %v", err)
	}
	if declared.GetJob().GetRole() != "" {
		t.Fatalf("the job runs as %q, want as nobody", declared.GetJob().GetRole())
	}
}

// What the controller reads is the role the workspace holds now, over the narrow interface: two
// answers to which role this is would check the boundary against one and apply it against another.
func TestTheControllerReadsTheRoleTheWorkspaceHolds(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	workspace, _ := newProject(t, s)
	importRoleReceiving(t, s, "backlog-clearer", 1, "job")
	attachRole(t, s, workspace, "backlog-clearer")

	held, err := s.RoleFor(context.Background(), workspace, "backlog-clearer")
	if err != nil {
		t.Fatalf("RoleFor: %v", err)
	}
	if !held.Gets(role.MaterialJob) || held.Gets(role.MaterialContext) {
		t.Fatal("the role the controller read is not the role the workspace holds")
	}

	// Detached, and the sentence the controller writes onto a job row carries no status wrapping.
	if _, err := s.DetachRole(context.Background(), &quaycrewv1.DetachRoleRequest{
		Workspace: workspace, Name: "backlog-clearer",
	}); err != nil {
		t.Fatalf("DetachRole: %v", err)
	}
	_, err = s.RoleFor(context.Background(), workspace, "backlog-clearer")
	if err == nil {
		t.Fatal("a role the workspace no longer holds was read back")
	}
	if !strings.Contains(err.Error(), "backlog-clearer") || strings.Contains(err.Error(), "rpc error") {
		t.Fatalf("the refusal says %q, want the sentence naming the role and nothing wrapped round it", err)
	}
}

// declareJobIn is a job declared in a mode, for the jobs that work in a repository: a repository is
// reached over the network, and only one mode runs a network command without asking.
func declareJobIn(t *testing.T, s *controlplane.Server, project, title, mode string) *quaycrewv1.Job {
	t.Helper()
	created, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: title, Brief: "open the bill and say when it is due", Mode: mode,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return created.GetJob()
}
