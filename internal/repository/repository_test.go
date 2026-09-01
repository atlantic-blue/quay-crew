package repository_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/repository"
)

// The address somebody typed and the address somebody pasted are one address.
func TestAnAddressIsKeptAsAnOwnerAndAName(t *testing.T) {
	for _, typed := range []string{
		"atlantic-blue/transcript",
		"  atlantic-blue/transcript  ",
		"https://github.com/atlantic-blue/transcript",
		"https://github.com/atlantic-blue/transcript/",
		"https://github.com/atlantic-blue/transcript.git",
	} {
		tidy := repository.Tidy(typed)
		if tidy != "atlantic-blue/transcript" {
			t.Errorf("%q is kept as %q, want atlantic-blue/transcript", typed, tidy)
		}
		if err := repository.Usable(tidy); err != nil {
			t.Errorf("%q was refused: %v", typed, err)
		}
	}
}

// Refused while the person who typed it is looking, because the alternative is a job that runs for
// an hour and then pushes nowhere.
func TestAnAddressThatIsNotAnOwnerAndANameIsRefused(t *testing.T) {
	for _, typed := range []string{"", "transcript", "atlantic-blue/", "/transcript", "a/b/c", "atlantic blue/x"} {
		err := repository.Usable(repository.Tidy(typed))
		if err == nil {
			t.Errorf("%q was accepted as a repository", typed)
			continue
		}
		if !strings.Contains(err.Error(), "atlantic-blue/quay-krewe") {
			t.Errorf("the refusal of %q says %q, want it to say what to type instead", typed, err)
		}
	}
}

func TestAnAddressAboveTheCeilingIsRefused(t *testing.T) {
	long := "owner/" + strings.Repeat("a", repository.Limit)
	err := repository.Usable(long)
	if err == nil {
		t.Fatal("an address above the ceiling was accepted")
	}
	if !strings.Contains(err.Error(), "200") {
		t.Fatalf("the refusal says %q, want it to name the ceiling", err)
	}
}

// Nothing said is public, and the reason is the cost. It is the whole point of recording the kind:
// the pipeline is free on one and metered on the other, and that was a sentence in a person's head.
func TestSayingNothingIsPublic(t *testing.T) {
	kind, err := repository.Kind("")
	if err != nil {
		t.Fatalf("saying nothing was refused: %v", err)
	}
	if kind != repository.Public {
		t.Fatalf("saying nothing gives %q, want public", kind)
	}
	if !strings.Contains(repository.Costs(kind), "free") {
		t.Errorf("a public repository says %q, want it to say the minutes are free", repository.Costs(kind))
	}
	if !strings.Contains(repository.Costs(repository.Private), "metered") {
		t.Errorf("a private repository says %q, want it to say the minutes are metered",
			repository.Costs(repository.Private))
	}
}

func TestTheKindIsReadHoweverItIsTyped(t *testing.T) {
	for typed, want := range map[string]string{
		"public": repository.Public, "PUBLIC": repository.Public, " public ": repository.Public,
		"private": repository.Private, "Private": repository.Private,
	} {
		got, err := repository.Kind(typed)
		if err != nil {
			t.Errorf("%q was refused: %v", typed, err)
			continue
		}
		if got != want {
			t.Errorf("%q reads as %q, want %q", typed, got, want)
		}
	}
}

// A word that is neither is refused rather than taken for the default. A forge has other kinds, and
// recording "internal" as public would be the system writing down a cost fact nobody told it.
func TestAKindThatIsNeitherIsRefused(t *testing.T) {
	for _, typed := range []string{"internal", "unlisted", "yes", "pubic"} {
		got, err := repository.Kind(typed)
		if err == nil {
			t.Errorf("%q was read as %q, want a refusal", typed, got)
			continue
		}
		for _, wants := range []string{"public", "private", typed} {
			if !strings.Contains(err.Error(), wants) {
				t.Errorf("the refusal of %q says %q, want it to say %q", typed, err, wants)
			}
		}
	}
}
