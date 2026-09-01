package controlplane_test

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// The one question the verification gap method asks, and the shape that ships wrong most often.
// Asserting on the file in roles/ is the unit tier's job. What this tier answers is different: does
// the sentence reach the session, through the store, the version a workspace pinned, and the
// renderer that writes the memory file the model opens.
const (
	theGapQuestion         = "If the behaviour this change produces broke where it is used, would verification fail?"
	theShapeThatShipsWrong = "A test that runs the changed code and never checks the changed result."
)

// verifying is a control plane with a workspace and a project, built the way one is built for real,
// and holding on to the storage so a test can read what a session was actually given.
type verifying struct {
	server    *controlplane.Server
	provider  *sandbox.FakeProvider
	storage   sandbox.Storage
	workspace string
	project   string
}

func aSystemThatVerifies(t *testing.T) *verifying {
	t.Helper()
	dir := t.TempDir()
	it := &verifying{
		provider: &sandbox.FakeProvider{},
		storage:  sandbox.Storage{Dir: dir, Host: dir},
	}
	it.server = controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: it.provider, Secrets: secrets.NewMemory(), Storage: it.storage,
	})
	ctx := context.Background()
	it.server.Seed(ctx, shippedSkills, slog.New(slog.DiscardHandler))

	workspace, err := it.server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "atlantic-blue"})
	if err != nil {
		t.Fatalf("create the workspace: %v", err)
	}
	it.workspace = workspace.GetWorkspace().GetId()
	project, err := it.server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: it.workspace, Name: "captions",
	})
	if err != nil {
		t.Fatalf("create the project: %v", err)
	}
	it.project = project.GetProject().GetId()
	return it
}

// hold gives the workspace a role and pins it, which is the pair of calls an operator makes. Attach
// is what moves a workspace onto a newer version, so it is called every time rather than once.
func (v *verifying) hold(t *testing.T, files []*quaycrewv1.RoleFile, named string) {
	t.Helper()
	ctx := context.Background()
	if _, err := v.server.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files}); err != nil {
		t.Fatalf("the system refused the %s role: %v", named, err)
	}
	if _, err := v.server.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: v.workspace, Name: named,
	}); err != nil {
		t.Fatalf("attach the %s role: %v", named, err)
	}
}

// briefGivenTo declares a job naming a role, runs the controller over it, and returns the memory
// file the session doing it reads.
//
// The file rather than the store, because the store is not what a model opens. A brief that reached
// the row and not the container is a method nothing ever reads. The task is detached, so the
// container and its files are built while this call is already past the declaration, and the read is
// waited for rather than taken once.
func (v *verifying) briefGivenTo(t *testing.T, named, title string) string {
	t.Helper()
	ctx := context.Background()
	declared, err := v.server.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: v.project, Title: title,
		Brief: "read the slice against its contracts", Role: named,
	})
	if err != nil {
		t.Fatalf("declare the job: %v", err)
	}
	v.server.TickJob(ctx)

	done, err := v.server.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("read the job: %v", err)
	}
	session := done.GetJob().GetSession()
	if session == "" {
		t.Fatalf("the job runs in no session, so nothing was given anything: %s", done.GetJob().GetReason())
	}

	var body string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		read, err := v.memoryOf(session)
		if err == nil && read != "" {
			body = read
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if body == "" {
		t.Fatalf("session %s was given no memory file, so there is no brief in front of it", session)
	}
	// A floor, not a measurement. An empty file carries no sentence of the method and also carries
	// nothing else, so without this a session told nothing would read as one told correctly by every
	// check below that only asks what is absent.
	if len(body) < 1024 {
		t.Fatalf("the memory file is %d bytes, so there is no brief in it to read:\n%s", len(body), body)
	}
	return body
}

// memoryOf is the file the session's own container reads, found through the configuration the system
// built that container from.
func (v *verifying) memoryOf(session string) (string, error) {
	for _, box := range v.provider.Configurations() {
		if box.ID != session {
			continue
		}
		dirs := v.storage.MyDirs(box)
		if len(dirs) == 0 {
			return "", fmt.Errorf("the session's sandbox has no memory directory")
		}
		body, found := sandbox.ReadMemory(dirs[0])
		if !found {
			return "", fmt.Errorf("nothing was written to %s", dirs[0])
		}
		return body, nil
	}
	return "", fmt.Errorf("no sandbox was built for session %s", session)
}

// theOldVerifier is the shipped role with the method taken back out and the version put back to 1,
// which is what the workspaces holding the previous version are still being given.
func theOldVerifier(t *testing.T) []*quaycrewv1.RoleFile {
	t.Helper()
	files := filesOf(t, filepath.Join(shippedRoles, "verifier"))
	for _, file := range files {
		body := string(file.GetBody())
		switch file.GetPath() {
		case role.ManifestFile:
			back := strings.Replace(body, fmt.Sprintf("version: %d", shippedVersionOf(t, "verifier")), "version: 1", 1)
			if back == body {
				t.Fatalf("the shipped verifier does not carry the version this test puts back:\n%s", body)
			}
			file.Body = []byte(back)
		case role.BriefFile:
			opens, closes := strings.Index(body, "<verification_gap>"), strings.Index(body, "</verification_gap>")
			if opens < 0 || closes < 0 {
				t.Fatalf("the shipped brief carries no verification gap section to take out")
			}
			file.Body = []byte(body[:opens] + body[closes+len("</verification_gap>"):])
		}
	}
	return files
}

// The sad path, first, because a check that cannot fail says nothing about the check that passes.
//
// This is the verifier as it shipped before: a session told to judge whether a slice works, and told
// no way of telling a green check from a real one. The read below finds nothing, which is what makes
// the same read finding something in the test after it mean anything.
func TestAVerifierWithoutTheMethodIsGivenNoWayToTellAGreenCheckFromARealOne(t *testing.T) {
	it := aSystemThatVerifies(t)
	it.hold(t, theOldVerifier(t), "verifier")

	brief := it.briefGivenTo(t, "verifier", "verify the rounding slice")

	if strings.Contains(brief, theGapQuestion) {
		t.Error("the brief with the section cut out still asks the question, so cutting it changed nothing")
	}
	if strings.Contains(brief, theShapeThatShipsWrong) {
		t.Error("the brief with the section cut out still names the test that checks no result")
	}
	// And the rest of the role did reach the session, so the two lines above are a missing method
	// rather than a missing brief.
	if !strings.Contains(brief, "You are the verifier.") {
		t.Errorf("the session was not given the verifier's brief at all:\n%s", brief)
	}
}

// The acceptance criterion, driven over the real control plane: a job naming the verifier is given
// the brief that carries the method. Nothing here reads roles/ for its answer. It reads the file the
// container holds, after the import, the attach, the pin and the renderer.
func TestAJobNamingTheVerifierIsGivenTheVerificationGapMethod(t *testing.T) {
	it := aSystemThatVerifies(t)
	it.hold(t, filesOf(t, filepath.Join(shippedRoles, "verifier")), "verifier")

	brief := it.briefGivenTo(t, "verifier", "verify the rounding slice")

	for _, want := range []string{
		theGapQuestion,
		theShapeThatShipsWrong,
		"A regression gap.",
		"A missing adoption gap.",
		"A broken verification gap.",
		"The file and the line of the behaviour that nothing protects.",
		"The search that grounds it",
		"bmad-code-org/BMAD-METHOD",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief in front of the session does not say %q", want)
		}
	}
	t.Logf("the session verifying was given %d bytes of brief", len(brief))
}

// The version, read back the way an operator reads it. A workspace that pinned the old verifier goes
// on being given the old one until somebody attaches again, which is the whole reason the number
// moved rather than the file changing under version 1.
func TestAWorkspaceHoldingTheOldVerifierIsMovedOnByAttachingAgain(t *testing.T) {
	it := aSystemThatVerifies(t)
	ctx := context.Background()
	it.hold(t, theOldVerifier(t), "verifier")

	if _, err := it.server.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: filesOf(t, filepath.Join(shippedRoles, "verifier")),
	}); err != nil {
		t.Fatalf("the system refused the verifier this build ships: %v", err)
	}
	held, err := it.server.ListRoles(ctx, &quaycrewv1.ListRolesRequest{Workspace: it.workspace})
	if err != nil {
		t.Fatalf("list what the workspace holds: %v", err)
	}
	if len(held.GetRoles()) != 1 || held.GetRoles()[0].GetVersion() != 1 {
		t.Fatalf("the workspace moved on its own: %+v", held.GetRoles())
	}

	if _, err := it.server.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: it.workspace, Name: "verifier",
	}); err != nil {
		t.Fatalf("attach the verifier again: %v", err)
	}
	moved, err := it.server.ListRoles(ctx, &quaycrewv1.ListRolesRequest{Workspace: it.workspace})
	if err != nil {
		t.Fatalf("list what the workspace holds: %v", err)
	}
	if moved.GetRoles()[0].GetVersion() != int32(shippedVersionOf(t, "verifier")) {
		t.Fatalf("the workspace holds version %d after attaching again", moved.GetRoles()[0].GetVersion())
	}
	if !strings.Contains(it.briefGivenTo(t, "verifier", "verify it again"), theGapQuestion) {
		t.Error("the workspace holds version 2 and its session is still given a brief without the question")
	}
}
