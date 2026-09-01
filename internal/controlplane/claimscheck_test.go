package controlplane_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/role"
)

// The claims check, driven over the control plane rather than read off disk. Asserting on the file
// in roles/ is the unit tier's job. What this tier answers is different: does the rule reach the
// session, through the import, the attach, the version a workspace pinned, and the renderer that
// writes the memory file the model opens.
const (
	theTestimonyRule = "The narrative is testimony, not evidence."
	theSilenceRule   = "A claim you could not falsify produces nothing."
)

// theVerifierWithoutTheClaimsCheck is the shipped role with the section taken back out and the
// version put back, which is what a workspace holding the previous version is still being given.
func theVerifierWithoutTheClaimsCheck(t *testing.T) []*quaycrewv1.RoleFile {
	t.Helper()
	files := filesOf(t, filepath.Join(shippedRoles, "verifier"))
	for _, file := range files {
		body := string(file.GetBody())
		switch file.GetPath() {
		case role.ManifestFile:
			back := strings.Replace(body, fmt.Sprintf("version: %d", shippedVersionOf(t, "verifier")), "version: 2", 1)
			if back == body {
				t.Fatalf("the shipped verifier does not carry the version this test puts back:\n%s", body)
			}
			file.Body = []byte(back)
		case role.BriefFile:
			opens, closes := strings.Index(body, "<claims_check>"), strings.Index(body, "</claims_check>")
			if opens < 0 || closes < 0 {
				t.Fatalf("the shipped brief carries no claims check to take out")
			}
			file.Body = []byte(body[:opens] + body[closes+len("</claims_check>"):])
		}
	}
	return files
}

// The sad path, first, because a check that cannot fail says nothing about the check that passes.
//
// This is the verifier as it shipped before: a session told to read a finished slice, and told
// nothing about the prose that came with it. The read below finds nothing, which is what makes the
// same read finding something in the test after it mean anything.
func TestAVerifierWithoutTheClaimsCheckIsToldNothingAboutWhatTheChangeSaysAboutItself(t *testing.T) {
	it := aSystemThatVerifies(t)
	it.hold(t, theVerifierWithoutTheClaimsCheck(t), "verifier")

	brief := it.briefGivenTo(t, "verifier", "verify the captions slice")

	if strings.Contains(brief, theTestimonyRule) {
		t.Error("the brief with the section cut out still calls the narrative testimony, so cutting it changed nothing")
	}
	if strings.Contains(brief, theSilenceRule) {
		t.Error("the brief with the section cut out still says a claim that survives produces nothing")
	}
	// And the rest of the role did reach the session, so the two reads above are a missing check
	// rather than a missing brief. The tracing method is the neighbour it was added next to.
	if !strings.Contains(brief, theGapQuestion) {
		t.Errorf("the session lost the tracing method too, so this is not the verifier with one section out:\n%s", brief)
	}
}

// The acceptance criterion. A job naming the verifier is given the brief that carries the claims
// check. Nothing here reads roles/ for its answer: it reads the file the container holds.
func TestAJobNamingTheVerifierIsGivenTheClaimsCheck(t *testing.T) {
	it := aSystemThatVerifies(t)
	it.hold(t, filesOf(t, filepath.Join(shippedRoles, "verifier")), "verifier")

	brief := it.briefGivenTo(t, "verifier", "verify the captions slice")

	for _, want := range []string{
		theTestimonyRule,
		"Extract each checkable claim.",
		"Try to falsify each one",
		"Read this section last.",
		"The file and the line where the code contradicts the claim.",
		"What the code does instead.",
		"claims-check.md",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief in front of the session does not say %q", want)
		}
	}
	t.Logf("the session verifying was given %d bytes of brief", len(brief))
}

// The half that a check like this loses first, and the half no test of a false claim can cover.
//
// A session that reports every claim it read passes any check that only asks whether a false claim
// is reported. So the rule that a surviving claim produces nothing is read on its own, from the same
// file the session opens.
func TestTheVerifierIsToldToSaySomethingOnlyAboutAClaimItBroke(t *testing.T) {
	it := aSystemThatVerifies(t)
	it.hold(t, filesOf(t, filepath.Join(shippedRoles, "verifier")), "verifier")

	brief := it.briefGivenTo(t, "verifier", "verify the captions slice")

	if !strings.Contains(brief, theSilenceRule) {
		t.Error("the brief does not say a claim that survives produces nothing, so a session obeying it reports every claim it read")
	}
	if !strings.Contains(brief, "A claim you could not falsify appears nowhere in this report.") {
		t.Error("the report the brief asks for has no place that says a surviving claim is left out")
	}
}

// The order, in the file the session actually opens. The tracing is finished before the prose is
// read, so a claim cannot steer the trace that would have caught it. Nothing enforces this at run
// time, so what ships is the instruction plus the position, and both are read here.
func TestTheSessionReadsTheClaimsAfterTheTracing(t *testing.T) {
	it := aSystemThatVerifies(t)
	it.hold(t, filesOf(t, filepath.Join(shippedRoles, "verifier")), "verifier")

	brief := it.briefGivenTo(t, "verifier", "verify the captions slice")

	tracing, claims := strings.Index(brief, theGapQuestion), strings.Index(brief, theTestimonyRule)
	if tracing < 0 || claims < 0 {
		t.Fatalf("the brief in front of the session holds the tracing at %d and the claims check at %d", tracing, claims)
	}
	if claims < tracing {
		t.Errorf("the session reads the claims at %d and the tracing at %d, so the prose arrives the wrong way round",
			claims, tracing)
	}
	if !strings.Contains(brief, "steers the trace") {
		t.Error("the brief orders the two and does not say why, so the next edit moves them back")
	}
}

// The version, read back the way an operator reads it. A workspace that pinned version 2 goes on
// being given version 2 until somebody attaches again, which is the whole reason the number moved
// rather than the file changing under the version already in use.
func TestAWorkspaceHoldingTheVerifierWithoutTheClaimsCheckIsMovedOnByAttachingAgain(t *testing.T) {
	it := aSystemThatVerifies(t)
	ctx := t.Context()
	it.hold(t, theVerifierWithoutTheClaimsCheck(t), "verifier")

	if _, err := it.server.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: filesOf(t, filepath.Join(shippedRoles, "verifier")),
	}); err != nil {
		t.Fatalf("the system refused the verifier this build ships: %v", err)
	}
	held, err := it.server.ListRoles(ctx, &quaycrewv1.ListRolesRequest{Workspace: it.workspace})
	if err != nil {
		t.Fatalf("list what the workspace holds: %v", err)
	}
	if len(held.GetRoles()) != 1 || held.GetRoles()[0].GetVersion() != 2 {
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
	if !strings.Contains(it.briefGivenTo(t, "verifier", "verify it again"), theTestimonyRule) {
		t.Error("the workspace holds the newer version and its session is still given a brief without the claims check")
	}
}
