package main

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/manual"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// aSystemWithASession stands a system up with one project and one session in it, and hands back the
// store so a case can put the session into the state it is about.
func aSystemWithASession(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, store.Store) {
	t.Helper()
	held := store.NewMemory()
	client := testClientWith(t, controlplane.Config{
		Store: held, Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "exec", "hello")
	return client, held
}

// theOnlySessionIn is the session one of the two listings holds. The listing is named, because
// archiving moves a session from one to the other and a case has to be able to ask about either.
func theOnlySessionIn(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, archived bool) *quaycrewv1.Session {
	t.Helper()
	listed, err := client.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{Archived: archived})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed.GetSessions()) != 1 {
		t.Fatalf("the system holds %d sessions in that listing, want 1", len(listed.GetSessions()))
	}
	return listed.GetSessions()[0]
}

// The whole point of the word: the finished sessions stop burying the live ones. Measured on 5
// September 2026 a system held 303 sessions and 282 of them were stopped.
func TestArchivingASessionTakesItOutOfTheListing(t *testing.T) {
	client, _ := aSystemWithASession(t)
	session := theOnlySessionIn(t, client, false)

	said := mustRun(t, client, "archive", session.GetId())
	// Nothing is deleted, and the output has to say so: a word that reads like a delete stops a
	// person from using it.
	if !strings.Contains(said, "still there") {
		t.Errorf("archiving does not say the record is kept: %q", said)
	}
	if !strings.Contains(said, "krewe unarchive") {
		t.Errorf("archiving does not name the way back: %q", said)
	}

	live := mustRun(t, client, "sessions")
	if strings.Contains(live, session.GetId()[:8]) {
		t.Errorf("the archived session is still in the default listing:\n%s", live)
	}
	putAway := mustRun(t, client, "sessions", "--archived")
	if !strings.Contains(putAway, session.GetId()[:8]) {
		t.Errorf("the archived listing does not hold it:\n%s", putAway)
	}
}

// A listing that falls from 296 rows to 4 with no explanation reads as lost data, and a person who
// reads it as lost data stops archiving.
func TestTheListingSaysWhatItIsHidingAndNamesTheFlag(t *testing.T) {
	client, _ := aSystemWithASession(t)
	session := theOnlySessionIn(t, client, false)

	// Nothing is hidden yet, so there is nothing to explain and the line is left out.
	if before := mustRun(t, client, "sessions"); strings.Contains(before, "and hidden") {
		t.Errorf("a system with nothing put away still explains itself:\n%s", before)
	}

	// A second session, so each listing has something to say about the other one.
	mustRun(t, client, "exec", "and another")
	mustRun(t, client, "archive", session.GetId())

	live := mustRun(t, client, "sessions")
	if !strings.Contains(live, "1 archived and hidden.") {
		t.Errorf("the default listing does not say what it is hiding:\n%s", live)
	}
	// The advice has to be typeable, so the count names the whole command rather than the flag alone.
	if !strings.Contains(live, "krewe sessions system --archived") {
		t.Errorf("the default listing does not name the command that shows them:\n%s", live)
	}

	// And the same the other way round, so neither listing is the one that explains itself.
	putAway := mustRun(t, client, "sessions", "--archived")
	if !strings.Contains(putAway, "1 live and hidden.") {
		t.Errorf("the archived listing does not say what it is hiding:\n%s", putAway)
	}
	if !strings.Contains(putAway, "krewe sessions system lists them") {
		t.Errorf("the archived listing does not name the way back to the live ones:\n%s", putAway)
	}
}

// It is a flag and not a setting. One command with two spellings is one command where a spelling
// drifts.
func TestTheArchivedFlagTakesNoValue(t *testing.T) {
	client, _ := aSystemWithASession(t)

	_, err := asked(t, client, "sessions", "--archived=true")
	if err == nil {
		t.Fatal("krewe sessions --archived=true was accepted, so the flag reads as a setting")
	}
	if !strings.Contains(err.Error(), "takes no value") {
		t.Errorf("the refusal does not say why: %s", err)
	}
	if !strings.Contains(err.Error(), "krewe sessions --archived") {
		t.Errorf("the refusal does not name what to type instead: %s", err)
	}
}

// The project form is a sweep. It takes what it can and says what it left, because a sweep that
// reports only what it took reads as a sweep that took everything.
func TestArchivingAProjectTakesTheSettledSessionsAndSaysWhatItLeft(t *testing.T) {
	client, held := aSystemWithASession(t)
	settled := theOnlySessionIn(t, client, false)
	if err := held.StopSession(context.Background(), settled.GetId()); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	// A second session, still holding its container, which the sweep must leave alone.
	mustRun(t, client, "exec", "and another")

	said := mustRun(t, client, "archive", "me/house-bills")
	if !strings.Contains(said, "archived 1 session") {
		t.Errorf("the sweep does not say what it took: %q", said)
	}
	if !strings.Contains(said, "1 session left in the listing") {
		t.Errorf("the sweep does not say what it left: %q", said)
	}
	if live := mustRun(t, client, "sessions"); strings.Contains(live, settled.GetId()[:8]) {
		t.Errorf("the stopped session is still in the default listing:\n%s", live)
	}
	if putAway := mustRun(t, client, "sessions", "--archived"); !strings.Contains(putAway, settled.GetId()[:8]) {
		t.Errorf("the archived listing does not hold what the sweep took:\n%s", putAway)
	}
}

// A wrong address hides work a person then cannot find, so the way back ships with the way in.
func TestUnarchivingPutsASessionBackAndSaysItHoldsNoContainer(t *testing.T) {
	client, _ := aSystemWithASession(t)
	session := theOnlySessionIn(t, client, false)
	mustRun(t, client, "archive", session.GetId())

	said := mustRun(t, client, "unarchive", session.GetId())
	if !strings.Contains(said, "back in the listing") {
		t.Errorf("unarchiving does not say the session is back: %q", said)
	}
	// An operator who expects the old container back finds a session that answers nothing.
	if !strings.Contains(said, "holds no container") {
		t.Errorf("unarchiving says nothing about the container: %q", said)
	}
	if back := theOnlySessionIn(t, client, false); back.GetStatus() != "stopped" {
		t.Errorf("the restored session reads %q, want stopped", back.GetStatus())
	}
}

// Archiving a session twice hides it once and then says so, naming the word that undoes it, rather
// than answering with a refusal from two layers down.
func TestArchivingAnArchivedSessionNamesTheWayBack(t *testing.T) {
	client, _ := aSystemWithASession(t)
	session := theOnlySessionIn(t, client, false)
	mustRun(t, client, "archive", session.GetId())

	_, err := asked(t, client, "archive", session.GetId())
	if err == nil {
		t.Fatal("archiving an archived session was accepted")
	}
	if !strings.Contains(err.Error(), "krewe unarchive") {
		t.Errorf("the refusal does not name the way back: %s", err)
	}
}

// A tool with a command its own help does not name is a tool nobody finds the command in.
func TestTheUsageNamesBothHalvesOfArchiving(t *testing.T) {
	for _, word := range []string{"archive [<address>] [<session>]", "unarchive <session>", "--archived"} {
		if !strings.Contains(manual.Commands, word) {
			t.Errorf("the usage does not name %q", word)
		}
	}
	// Neither word is retired, so no refusal table is made to lie about a command the tool has.
	for _, word := range []string{"archive", "unarchive"} {
		if _, gone := removedCommands[word]; gone {
			t.Errorf("krewe %s is both a command and a removed word", word)
		}
	}
	if _, gone := removedFlags[flagArchived]; gone {
		t.Errorf("%s is both a flag the listing takes and a removed one", flagArchived)
	}
}

// A sweep over a project where nothing has finished takes nothing, and has to say so: silence reads
// as a sweep that worked.
func TestArchivingAProjectWhereEverySessionIsLiveSaysItTookNothing(t *testing.T) {
	client, _ := aSystemWithASession(t)

	said := mustRun(t, client, "archive", "me/house-bills")
	if !strings.Contains(said, "nothing was archived") {
		t.Errorf("the sweep does not say it took nothing: %q", said)
	}
	if !strings.Contains(said, "1 session left in the listing") {
		t.Errorf("the sweep does not say what it left: %q", said)
	}
	live := theOnlySessionIn(t, client, false)
	if listed := mustRun(t, client, "sessions"); !strings.Contains(listed, live.GetId()[:8]) {
		t.Errorf("the live session did not stay in the listing:\n%s", listed)
	}
}

// An address that stops at a workspace names neither of the two forms, and archiving a whole
// workspace is not a thing this word does.
func TestArchivingAWorkspaceIsRefusedAndNamesTheTwoForms(t *testing.T) {
	client, _ := aSystemWithASession(t)

	_, err := asked(t, client, "archive", "me")
	if err == nil {
		t.Fatal("krewe archive me was accepted, so a workspace reads as something to archive")
	}
	if !strings.Contains(err.Error(), "names a workspace") {
		t.Errorf("the refusal does not say what went wrong: %s", err)
	}
	if !strings.Contains(err.Error(), "krewe archive") {
		t.Errorf("the refusal does not show the forms it takes: %s", err)
	}
}
