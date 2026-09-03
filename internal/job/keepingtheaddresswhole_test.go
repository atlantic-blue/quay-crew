package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A job names the repository its work goes to, at whatever length that address is.
//
// The rule about an address is one rule and it lives in internal/repository, so a job holds to the
// same one. What a job adds is where the refusal lands: a declaration refused for the length of its
// address never runs at all, and the person who typed it is asked to shorten a name they do not own.

// aRepositoryAddressOf is an owner and a name of exactly this many bytes, so the shape holds and
// only the length is unusual.
func aRepositoryAddressOf(size int) string {
	const owner = "atlantic-blue/"
	name := size - len(owner)
	if name < 1 {
		panic("an address this short cannot carry an owner and a name")
	}
	return owner + "quay-krewe" + strings.Repeat("x", name-len("quay-krewe"))
}

func TestAJobNamingALongRepositoryIsAcceptedAndKeepsTheAddress(t *testing.T) {
	address := aRepositoryAddressOf(job.RepositoryLimit * 2)
	one := job.Declaration{
		Title:      "move the caps that still refuse text",
		Brief:      "sweep every site and keep the words",
		Repository: address,
	}

	if err := one.Validate(); err != nil {
		t.Fatalf("a job whose repository is %d bytes was refused: %v", len(address), err)
	}
	if kept := one.Tidied().Repository; kept != address {
		t.Fatalf("the repository was kept as %q, and it was declared as %q", kept, address)
	}
}
