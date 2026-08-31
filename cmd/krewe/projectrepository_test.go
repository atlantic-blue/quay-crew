package main

import (
	"context"
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

// The fault this command was rebuilt around. An operator standing in one project typed the address
// of another to read what it works in, and the tool read that address as a repository and wrote it
// to the project in scope. The answer was the sentence a read prints, so the overwrite read as a
// confirmation. Refused before anything else here, because a gate that always opens satisfies every
// test about opening.
func TestOneArgumentThatNamesAProjectIsRefusedWithBothReadings(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	// The operator is standing in the second project, which is the one the command never mentions
	// and the one the fault damaged.
	mustRun(t, client, "project", "create", "other")
	mustRun(t, client, "project", "repository", "forge/other-repo", "private")

	said, err := runKrewe(t, client, "project", "repository", "acme/house-bills")
	if err == nil {
		t.Fatalf("the ambiguous command was accepted, and said %q", said)
	}
	// Both readings, and the spelling of each, because the operator has to be able to type the one
	// they meant.
	for _, wants := range []string{
		"acme/house-bills",
		"krewe project repository show acme/house-bills",
		"krewe project repository acme/other acme/house-bills",
	} {
		if !strings.Contains(err.Error(), wants) {
			t.Errorf("the refusal says %q, want it to say %q", err, wants)
		}
	}
	// The project in scope is the one that was about to be overwritten, so the refusal names it.
	if !strings.Contains(err.Error(), "acme/other") {
		t.Errorf("the refusal says %q, want it to name the project in scope", err)
	}

	// And nothing was written. The repository the project held, and its kind, are the ones it
	// started with.
	held := mustRun(t, client, "project", "repository", "show", "acme/other")
	for _, wants := range []string{"forge/other-repo", "private", "metered"} {
		if !strings.Contains(held, wants) {
			t.Errorf("the refused command changed the project in scope: %q, want it to say %q", held, wants)
		}
	}
	// The project the operator named was not touched either.
	named := mustRun(t, client, "project", "repository", "show", "acme/house-bills")
	if !strings.Contains(named, "no repository") {
		t.Errorf("the refused command wrote to the project it named: %q", named)
	}

	// And the record itself, read off the control plane rather than out of a second command's prose,
	// because the prose is the half that was lying in this fault.
	if held := theProject(t, client, "other"); held.GetRepository() != "forge/other-repo" ||
		held.GetVisibility() != "private" {
		t.Errorf("the system holds %q, %q for the project in scope, want forge/other-repo, private",
			held.GetRepository(), held.GetVisibility())
	}
	if held := theProject(t, client, "house-bills"); held.GetRepository() != "" {
		t.Errorf("the system holds %q for the project the command named, want nothing", held.GetRepository())
	}
}

// The kind does not make the argument a repository. An operator who types an address and a kind may
// still have meant the project by that address, and the two argument form is one word away.
func TestOneArgumentThatNamesAProjectIsRefusedWithAKindToo(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "project", "create", "other")

	said, err := runKrewe(t, client, "project", "repository", "acme/house-bills", "private")
	if err == nil {
		t.Fatalf("the ambiguous command was accepted, and said %q", said)
	}
	// The spelling it hands back keeps the kind, so the operator retypes nothing.
	if !strings.Contains(err.Error(), "krewe project repository acme/other acme/house-bills private") {
		t.Errorf("the refusal says %q, want it to carry the kind into the spelling it offers", err)
	}
}

// The way out of the refusal has to work, or the refusal is a locked door. Two arguments are
// unambiguous by position, so an address that also names a project is recorded there.
func TestTheSpellingTheRefusalOffersRecordsTheRepository(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "project", "create", "other")

	mustRun(t, client, "project", "repository", "acme/other", "acme/house-bills")

	held := mustRun(t, client, "project", "repository", "show", "acme/other")
	if !strings.Contains(held, "acme/house-bills") {
		t.Errorf("the two argument form did not record the repository: %q", held)
	}
}

// One argument that names nothing this system holds is the ordinary case, and it still records.
func TestOneArgumentThatNamesNoProjectIsStillRecorded(t *testing.T) {
	client := testClient(t)
	aProject(t, client)

	// A workspace that exists with no project of that name under it is still not a project.
	mustRun(t, client, "project", "repository", "me/nothing-here")

	held := mustRun(t, client, "project", "repository")
	if !strings.Contains(held, "me/nothing-here") {
		t.Errorf("one argument naming no project did not record it: %q", held)
	}
}

// The intent that had no spelling. Reading another project's repository was only reachable through
// the form that records one, which is why the operator reached for the form that destroyed data.
func TestAnotherProjectIsReadWithoutWritingAnything(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "project", "repository", "atlantic-blue/transcript", "private")
	mustRun(t, client, "project", "create", "other")
	mustRun(t, client, "project", "repository", "forge/other-repo")

	read := mustRun(t, client, "project", "repository", "show", "acme/house-bills")
	for _, wants := range []string{"atlantic-blue/transcript", "private", "metered"} {
		if !strings.Contains(read, wants) {
			t.Errorf("the read says %q, want it to say %q", read, wants)
		}
	}
	// Nothing moved on either project.
	if held := mustRun(t, client, "project", "repository", "show", "acme/other"); !strings.Contains(held, "forge/other-repo") {
		t.Errorf("the read wrote to the project in scope: %q", held)
	}
	if here := mustRun(t, client, "project", "repository"); !strings.Contains(here, "forge/other-repo") {
		t.Errorf("the project in scope moved: %q", here)
	}
}

// A read with no address is the project the operator is standing in, the way it always was.
func TestTheReadWithNoAddressIsTheProjectInScope(t *testing.T) {
	client := testClient(t)
	aProject(t, client)
	mustRun(t, client, "project", "repository", "atlantic-blue/transcript")

	if read := mustRun(t, client, "project", "repository", "show"); !strings.Contains(read, "atlantic-blue/transcript") {
		t.Errorf("the read says %q, want it to read the project in scope", read)
	}
}

// A read records nothing, so a kind on it is a write that lost its address.
func TestAReadWithAKindIsRefused(t *testing.T) {
	client := testClient(t)
	aProject(t, client)

	said, err := runKrewe(t, client, "project", "repository", "show", "me/transcript", "private")
	if err == nil {
		t.Fatalf("a read with a kind was accepted, and said %q", said)
	}
	if !strings.Contains(err.Error(), "krewe project repository show") {
		t.Errorf("the refusal says %q, want it to say how a read is typed", err)
	}
}

// A workspace has no repository, and the refusal off a read says how a read is typed rather than how
// a write is.
func TestAReadOnAWorkspaceSaysARepositoryBelongsToAProject(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")

	said, err := runKrewe(t, client, "project", "repository", "show", "me")
	if err == nil {
		t.Fatalf("a read on a workspace was accepted, and said %q", said)
	}
	if !strings.Contains(err.Error(), "krewe project repository show <workspace>/<project>") {
		t.Errorf("the refusal says %q, want it to say how a read is typed", err)
	}
}

// The line that turned a mistake into a confirmation. A read and a write said one sentence between
// them, so the write now says it wrote, and what it wrote over.
func TestAWriteSaysItWroteAndWhatItChangedFrom(t *testing.T) {
	client := testClient(t)
	aProject(t, client)

	first := mustRun(t, client, "project", "repository", "atlantic-blue/transcript", "private")
	if !strings.Contains(first, "recorded") {
		t.Errorf("the write says %q, want it to say it recorded something", first)
	}
	if !strings.Contains(first, "no repository before") {
		t.Errorf("the write says %q, want it to say the project held nothing before", first)
	}

	second := mustRun(t, client, "project", "repository", "atlantic-blue/videos")
	if !strings.Contains(second, "atlantic-blue/transcript") {
		t.Errorf("the write says %q, want it to say what it changed from", second)
	}

	read := mustRun(t, client, "project", "repository")
	if strings.Contains(read, "recorded") {
		t.Errorf("the read says %q, which is the line a write prints", read)
	}
	if read == second {
		t.Errorf("a read and a write print the same line: %q", read)
	}
}

// Correcting an address is not a statement about the bill. The kind used to be cleared by any write
// that did not repeat it, so a project fell from private to public and the tool said the minutes
// were free.
func TestAWriteWithNoKindKeepsTheKindTheProjectHolds(t *testing.T) {
	client := testClient(t)
	aProject(t, client)
	mustRun(t, client, "project", "repository", "atlantic-blue/transcript", "private")

	said := mustRun(t, client, "project", "repository", "atlantic-blue/videos")
	for _, wants := range []string{"private", "metered"} {
		if !strings.Contains(said, wants) {
			t.Errorf("the write says %q, want it to keep the kind the project holds (%q)", said, wants)
		}
	}
	if read := mustRun(t, client, "project", "repository"); !strings.Contains(read, "private") {
		t.Errorf("the project reads back as %q, want it to have kept its kind", read)
	}
	// And the kind is still a thing the operator can change, in the word that says so.
	back := mustRun(t, client, "project", "repository", "atlantic-blue/videos", "public")
	if !strings.Contains(back, "public") || !strings.Contains(back, "free") {
		t.Errorf("the kind could not be changed back: %q", back)
	}
}

// What each shape of the command means, in one table, because the fault was a shape being read as
// another shape.
func TestTheCommandReadsItsArguments(t *testing.T) {
	for _, one := range []struct {
		what    string
		typed   []string
		wrote   string
		kind    string
		refused bool
	}{
		{what: "no argument reads and records nothing", typed: nil},
		{what: "one address records it here", typed: []string{"forge/one"}, wrote: "forge/one", kind: "public"},
		{what: "one address and a kind record both", typed: []string{"forge/one", "private"},
			wrote: "forge/one", kind: "private"},
		{what: "two addresses record the second in the first", typed: []string{"acme/house-bills", "forge/one"},
			wrote: "forge/one", kind: "public"},
		{what: "two addresses and a kind record all three",
			typed: []string{"acme/house-bills", "forge/one", "private"}, wrote: "forge/one", kind: "private"},
		{what: "a kind on its own is refused", typed: []string{"private"}, refused: true},
		{what: "three addresses are refused", typed: []string{"a/b", "c/d", "e/f"}, refused: true},
		{what: "an address that also names a project is refused", typed: []string{"acme/house-bills"}, refused: true},
	} {
		t.Run(one.what, func(t *testing.T) {
			client := testClient(t)
			mustRun(t, client, "workspace", "create", "acme")
			mustRun(t, client, "project", "create", "house-bills")

			said, err := runKrewe(t, client, append([]string{"project", "repository"}, one.typed...)...)
			switch {
			case one.refused && err == nil:
				t.Fatalf("krewe project repository %s was accepted, and said %q", strings.Join(one.typed, " "), said)
			case !one.refused && err != nil:
				t.Fatalf("krewe project repository %s was refused: %v", strings.Join(one.typed, " "), err)
			}

			held := mustRun(t, client, "project", "repository", "show", "acme/house-bills")
			if one.wrote == "" {
				if !strings.Contains(held, "no repository") {
					t.Fatalf("the project works in %q, want nothing", held)
				}
				return
			}
			if !strings.Contains(held, one.wrote) {
				t.Errorf("the project works in %q, want %q", held, one.wrote)
			}
			if !strings.Contains(held, one.kind) {
				t.Errorf("the project reads back as %q, want the kind %q", held, one.kind)
			}
		})
	}
}

// theProject reads one project out of the system by name, so an assertion about what survived a
// refusal is about the record and not about what a second command printed.
func theProject(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, name string) *quaycrewv1.Project {
	t.Helper()
	resp, err := client.ListProjects(context.Background(), &quaycrewv1.ListProjectsRequest{})
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	for _, held := range resp.GetProjects() {
		if held.GetName() == name {
			return held
		}
	}
	t.Fatalf("this system holds no project named %q", name)
	return nil
}
