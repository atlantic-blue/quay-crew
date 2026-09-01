package forge

import (
	"context"
	"fmt"
	"sync"
)

// Fake is a forge a test writes the answers of.
//
// It ships beside the real reader rather than inside a test file because three tiers need it: the
// unit tier, the control plane's own tests and the scenarios in features. A double that only one of
// them could reach would be a second double, and two doubles are two chances to be looser than the
// service they stand for.
//
// It is deliberately no looser than GitHub. An address nothing was said about is a refusal, the way
// a pull request the credential cannot see is, so a test that forgot to say anything reads a refusal
// rather than an empty reading that looks like a pass.
type Fake struct {
	mu sync.Mutex
	// says is what this forge answers, keyed by the address as it is written.
	says map[string]Reading
	// refuses is why it will not answer about an address, which is how a test drives the read that
	// failed.
	refuses map[string]string
	// asked counts the calls, so a test can prove a settled pull request was not read again rather
	// than infer it from a row that would look the same either way.
	asked map[string]int
}

var _ Reader = (*Fake)(nil)

// NewFake is a forge that has been told nothing.
func NewFake() *Fake {
	return &Fake{says: map[string]Reading{}, refuses: map[string]string{}, asked: map[string]int{}}
}

// Says makes this forge answer one address with one reading.
func (f *Fake) Says(address string, reading Reading) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.says[address] = reading
	delete(f.refuses, address)
	return f
}

// Refuses makes this forge turn one address down, with a reason.
func (f *Fake) Refuses(address, why string) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refuses[address] = why
	delete(f.says, address)
	return f
}

// Asked is how many times this forge was asked about one address.
func (f *Fake) Asked(address string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked[address]
}

// Read answers what this forge was told to answer.
func (f *Fake) Read(_ context.Context, at Address) (Reading, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	address := at.String()
	f.asked[address]++
	if why, refused := f.refuses[address]; refused {
		return Reading{}, fmt.Errorf("%s", why)
	}
	reading, held := f.says[address]
	if !held {
		return Reading{}, fmt.Errorf("nothing holds a pull request at %s", address)
	}
	return reading, nil
}
