package main

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
)

// The command that says where a project's work lands.
//
// Typed at the moment the operator would otherwise have said it out loud, which in the acceptance
// run was "it should be a public repository so we can use the CI".

// aProject stands up a workspace and a project and leaves the tool standing in it.
func aProject(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) {
	t.Helper()
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "transcript")
}

func TestAProjectIsToldWhichRepositoryItsWorkLandsIn(t *testing.T) {
	client := testClient(t)
	aProject(t, client)

	said := mustRun(t, client, "project", "repository", "atlantic-blue/transcript")
	if !strings.Contains(said, "atlantic-blue/transcript") {
		t.Errorf("it does not say the repository: %q", said)
	}
	// The kind and its reason, in the same breath as the address, because the cost is the whole
	// reason the kind is recorded at all.
	for _, wants := range []string{"public", "free"} {
		if !strings.Contains(said, wants) {
			t.Errorf("it does not say %q: %q", wants, said)
		}
	}

	// And it is read back, by the command on its own and by the listing.
	read := mustRun(t, client, "project", "repository")
	if !strings.Contains(read, "atlantic-blue/transcript") {
		t.Errorf("reading it back does not say the repository: %q", read)
	}
	listed := mustRun(t, client, "project", "list")
	if !strings.Contains(listed, "atlantic-blue/transcript") {
		t.Errorf("the listing does not say the repository: %q", listed)
	}
}

// A private repository is the deliberate one, so the system says what it costs.
func TestAPrivateRepositorySaysItsMinutesAreMetered(t *testing.T) {
	client := testClient(t)
	aProject(t, client)

	said := mustRun(t, client, "project", "repository", "atlantic-blue/transcript", "private")
	for _, wants := range []string{"private", "metered"} {
		if !strings.Contains(said, wants) {
			t.Errorf("it does not say %q: %q", wants, said)
		}
	}
}

// The address in front of the repository says which project, the way it does everywhere else.
func TestARepositoryIsRecordedAgainstTheProjectTheAddressNames(t *testing.T) {
	client := testClient(t)
	aProject(t, client)
	mustRun(t, client, "project", "create", "house-bills")

	mustRun(t, client, "project", "repository", "me/transcript", "atlantic-blue/transcript")

	listed := mustRun(t, client, "project", "list")
	if !strings.Contains(listed, "atlantic-blue/transcript") {
		t.Fatalf("the listing does not say the repository: %q", listed)
	}
	// The project the operator is standing in is house-bills, and it was not the one named.
	said := mustRun(t, client, "project", "repository")
	if strings.Contains(said, "atlantic-blue/transcript") {
		t.Errorf("the repository landed on the project the operator was standing in: %q", said)
	}
}

// A project with no repository says so, and says what to type. The gap is the finding: a project
// that looks complete and has nowhere to push is the one that costs a session.
func TestAProjectWithNoRepositorySaysWhatToType(t *testing.T) {
	client := testClient(t)
	created := mustRun(t, client, "workspace", "create", "me")
	created += mustRun(t, client, "project", "create", "transcript")

	for _, said := range []string{
		created,
		mustRun(t, client, "project", "repository"),
		mustRun(t, client, "project", "list"),
	} {
		if !strings.Contains(said, "no repository") {
			t.Errorf("it does not say the project has no repository: %q", said)
		}
		if !strings.Contains(said, "krewe project repository") {
			t.Errorf("it does not say what to type: %q", said)
		}
	}
}

func TestARepositoryThatIsNotAnOwnerAndANameIsRefusedByTheTool(t *testing.T) {
	client := testClient(t)
	aProject(t, client)

	said, err := runKrewe(t, client, "project", "repository", "transcript")
	if err == nil {
		t.Fatalf("an address that is not an owner and a name was accepted, and said %q", said)
	}
	if !strings.Contains(err.Error(), "atlantic-blue/quay-crew") {
		t.Errorf("the refusal says %q, want it to say what to type instead", err)
	}
}

func TestAKindOfRepositoryTheSystemDoesNotKnowIsRefusedByTheTool(t *testing.T) {
	client := testClient(t)
	aProject(t, client)

	said, err := runKrewe(t, client, "project", "repository", "atlantic-blue/transcript", "internal")
	if err == nil {
		t.Fatalf("an unknown kind was accepted, and said %q", said)
	}
	for _, wants := range []string{"public", "private"} {
		if !strings.Contains(err.Error(), wants) {
			t.Errorf("the refusal says %q, want it to say %q", err, wants)
		}
	}
}

// A kind with no address in front of it is a command missing its argument, not a repository called
// "public". Absorbed silently, this would record a project as working in nowhere.
func TestAKindWithNoRepositoryIsRefused(t *testing.T) {
	client := testClient(t)
	aProject(t, client)

	said, err := runKrewe(t, client, "project", "repository", "public")
	if err == nil {
		t.Fatalf("a kind with no repository was accepted, and said %q", said)
	}
	if !strings.Contains(err.Error(), "usage: krewe project repository") {
		t.Errorf("the refusal says %q, want it to say how the command is typed", err)
	}
}

// The way off the command that used to be here. It is still in somebody's fingers, so it names both
// halves of what replaced it: the clone is a conversation, and the record is this command.
func TestTheRemoteCommandNamesTheRecordAndTheGitSkill(t *testing.T) {
	client := testClient(t)
	aProject(t, client)

	for _, args := range [][]string{
		{"project", "remote", "set", "https://github.com/atlantic-blue/transcript.git"},
		{"project", "remote"},
		{"repository", "add", "https://github.com/atlantic-blue/transcript.git"},
	} {
		said, err := runKrewe(t, client, args...)
		if err == nil {
			t.Errorf("krewe %s was accepted, and said %q", strings.Join(args, " "), said)
			continue
		}
		if !strings.Contains(err.Error(), "krewe project repository") {
			t.Errorf("krewe %s is refused with %q, want it to name the record", strings.Join(args, " "), err)
		}
		if !strings.Contains(err.Error(), "git skill") {
			t.Errorf("krewe %s is refused with %q, want it to name the git skill", strings.Join(args, " "), err)
		}
	}
}

// A workspace is not a project, and a repository belongs to a project. Standing in a workspace with
// no project under it, the refusal says which of the two the operator is holding.
func TestARepositoryOnAWorkspaceSaysItBelongsToAProject(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")

	said, err := runKrewe(t, client, "project", "repository", "atlantic-blue/transcript")
	if err == nil {
		t.Fatalf("a repository on a workspace was accepted, and said %q", said)
	}
	if !strings.Contains(err.Error(), "a repository belongs to a project") {
		t.Errorf("the refusal says %q, want it to say a repository belongs to a project", err)
	}
}
