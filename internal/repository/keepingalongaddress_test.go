package repository_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/repository"
)

// An address longer than the guide is still an owner and a name, and it is kept.
//
// A long address is usually a paste of something else, and that is a reason to say so rather than a
// reason to refuse the work. The shape is the rule that decides whether the system can act on an
// address, and the shape holds at any length: a job whose repository is refused for its length
// cannot be declared at all, and the person who typed it is told to shorten a name they do not own.

// anAddressOf is an owner and a name of exactly this many bytes, so the shape holds and only the
// length is unusual.
func anAddressOf(size int) string {
	const owner = "atlantic-blue/"
	name := size - len(owner)
	if name < 1 {
		panic("an address this short cannot carry an owner and a name")
	}
	return owner + "quay-krewe" + strings.Repeat("x", name-len("quay-krewe"))
}

func TestAnAddressOfAnyLengthIsAcceptedAndKeptWordForWord(t *testing.T) {
	address := anAddressOf(repository.Limit * 2)

	if err := repository.Usable(address); err != nil {
		t.Fatalf("an address of %d bytes was refused: %v", len(address), err)
	}
	if kept := repository.Tidy(address); kept != address {
		t.Fatalf("the address was kept as %q, and it was written as %q", kept, address)
	}
}

// The one refusal that stays is about shape rather than length: an address the system cannot act on
// is still refused while the person who typed it is looking, and the refusal says what is wrong with
// it rather than how long it is.
func TestALongAddressThatIsNotAnOwnerAndANameIsRefusedForItsShape(t *testing.T) {
	address := "https://example.test/" + anAddressOf(repository.Limit*2) + "/tree/main/one/two/three"

	err := repository.Usable(address)
	if err == nil {
		t.Fatalf("%q was accepted as a repository, and nothing can be pushed to it", address)
	}
	if !strings.Contains(err.Error(), "is not an owner and a name") {
		t.Fatalf("the refusal says %q, want it to say the address is not an owner and a name", err)
	}
}
