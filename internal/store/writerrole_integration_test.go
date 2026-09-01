//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// A writing job, run as the writer, over the real database and the real control plane.
//
// The unit tier reads roles/writer off disk and holds the brief to carrying the method. What only
// this tier reaches is the crossing that is the whole point of the role: the brief goes into a
// column, a different call reads it back, and the memory file the session is finally told to work by
// either carries the method or does not. If it does, a job's own brief can be the subject and the
// material and nothing else, which is what this role was written to buy.

// aSystemThatWrites stands the control plane up on a real database with somewhere to render a memory
// file, because the file is what this test reads. Without Storage the render is skipped and every
// assertion below would be about a file nobody wrote.
func aSystemThatWrites(t *testing.T, reply string) (*controlplane.Server, *sandbox.FakeProvider, string) {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	dir := t.TempDir()
	boxes := &sandbox.FakeProvider{}
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: &model.FakeRunner{Reply: reply}, Provider: boxes,
		Secrets: secrets.NewMemory(), Storage: sandbox.Storage{Dir: dir, Host: dir},
	}), boxes, dir
}

// importWriter puts the writer this build ships in front of the workspace, optionally with one of its
// sentences taken out, which is how the sad path below plants a brief that lost a refusal.
func importWriter(t *testing.T, s *controlplane.Server, workspace, cut string) {
	t.Helper()
	ctx := context.Background()
	read, err := role.ReadDir("../../roles/writer")
	if err != nil {
		t.Fatalf("reading the writer this build ships: %v", err)
	}
	files := make([]*quaycrewv1.RoleFile, 0, len(read))
	for _, file := range read {
		body := file.Body
		if file.Path == role.BriefFile && cut != "" {
			shortened := strings.Replace(string(body), cut, "", 1)
			if shortened == string(body) {
				t.Fatalf("the brief does not carry %q, so cutting it plants nothing", cut)
			}
			body = []byte(shortened)
		}
		files = append(files, &quaycrewv1.RoleFile{Path: file.Path, Body: body})
	}
	if _, err := s.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files}); err != nil {
		t.Fatalf("the system refused the writer, which ships with it: %v", err)
	}
	if _, err := s.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{Workspace: workspace, Name: "writer"}); err != nil {
		t.Fatalf("AttachRole: %v", err)
	}
}

// theShortBrief is a writing job as it is meant to look once the role exists: the subject, and the
// material the piece is written from. No voice rules, no word list, no instruction about citing a
// figure or stating a cost. Everything the old thousand word brief carried is now the role's.
const theShortBrief = `Write one post about the sandbox eviction work for the personal site.

Material, and the only place a figure may come from:
- the change landed on 12 August 2026
- 6 jobs were lost when the container runtime went down
- the eviction loop was not built, and a machine that fills up still has no way back`

// roleMemoryOf reads the memory file the session running as a role was told to work by. It is the
// outer of the two files, which is where the brief is rendered.
func roleMemoryOf(t *testing.T, boxes *sandbox.FakeProvider, dir, session string) string {
	t.Helper()
	box, found := sandboxFor(boxes, session)
	if !found {
		t.Fatalf("the system built no sandbox for session %s", session)
	}
	dirs := sandbox.Storage{Dir: dir, Host: dir}.MyDirs(box)
	if len(dirs) == 0 {
		t.Fatal("the session has no memory directory, so there is no brief to read")
	}
	body, held := sandbox.ReadMemory(dirs[0])
	if !held {
		t.Fatal("nothing was written to the session's memory file at all")
	}
	return body
}

// theMethod is what a session running as the writer is told, and what a job's brief therefore does
// not have to say. The two refusals first, because a role that accepts everything satisfies every
// test about producing a draft.
var theMethod = []string{
	"A figure that is not in the material does not go in the piece",
	"A draft that states no cost is not a draft",
	"Read the voice specification in full before you write a word.",
	"No dash as punctuation.",
	"The surface decides the length and the pronoun.",
}

// The sad path, first, and it is the proof that the happy path reads something. A writer whose brief
// lost a refusal is a session that was never told to refuse, and the file says so on the far side of
// the database. Without this, a memory file that came back empty would satisfy every "carries the
// method" assertion by carrying nothing anybody looked for.
func TestAWriterWhoseBriefLostARefusalTellsTheSessionNothingAboutItInPostgres(t *testing.T) {
	s, boxes, dir := aSystemThatWrites(t, "a draft")
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	gone := "A figure that is not in the material does not go in the piece"
	importWriter(t, s, workspace, gone)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "write the eviction post", Brief: theShortBrief, Role: "writer",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	done := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseDone)
	told := roleMemoryOf(t, boxes, dir, done.GetSession())

	if strings.Contains(told, gone) {
		t.Fatalf("the session was told %q and the imported brief does not carry it, so this file is not the brief", gone)
	}
	// And the rest of the method did survive, so the assertion above is about one sentence rather
	// than about a file that arrived empty.
	if !strings.Contains(told, "A draft that states no cost is not a draft") {
		t.Fatal("the session was told neither refusal, so the memory file carries no brief at all and proves nothing")
	}
	t.Logf("the session was told %d bytes, and not the refusal that was cut", len(told))
}

// The whole of what this slice buys. A job declared with the role carries the subject and the
// material, the session is told the method by the role, and a draft comes back.
func TestAWritingJobRunsAsTheWriterFromASubjectAndItsMaterialInPostgres(t *testing.T) {
	const draft = "The container runtime went down on 12 August 2026 and took 6 jobs with it. " +
		"What is still missing: nothing evicts, so a machine that fills up has no way back."
	s, boxes, dir := aSystemThatWrites(t, draft)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	importWriter(t, s, workspace, "")

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "write the eviction post", Brief: theShortBrief, Role: "writer",
		Requires: []string{"context"},
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	done := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseDone)
	if done.GetRole() != "writer" {
		t.Fatalf("the job ran as %q", done.GetRole())
	}
	if !strings.Contains(done.GetAnswer(), draft) {
		t.Errorf("the job answered %q", done.GetAnswer())
	}

	// The session ran as the writer, which is what decides which brief it was given.
	session, err := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: done.GetSession()})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.GetSession().GetRole() != "writer" {
		t.Errorf("the session runs as %q", session.GetSession().GetRole())
	}

	// The brief the job carries says what to write about and what the figures are, and says nothing
	// about how to write. That is the acceptance criterion, and it is checked against the row the
	// database holds rather than against the constant above.
	held, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	brief := held.GetJob().GetBrief()
	for _, rule := range theMethod {
		if strings.Contains(brief, rule) {
			t.Errorf("the job's own brief types out %q, which is the role's to carry", rule)
		}
	}
	if !strings.Contains(brief, "6 jobs were lost") {
		t.Errorf("the job's brief carries no material: %q", brief)
	}

	// And the session was told the method anyway, out of the role, through the database.
	told := roleMemoryOf(t, boxes, dir, done.GetSession())
	for _, rule := range theMethod {
		if !strings.Contains(told, rule) {
			t.Errorf("the session was never told %q, so the next writing job types it into its own brief", rule)
		}
	}
	t.Logf("a brief of %d bytes reached a session told %d bytes of method", len(brief), len(told))
}
