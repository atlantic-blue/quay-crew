package session

import (
	"context"
	"io"
	"strings"
)

// Fake is a Runtime for tests. It records the last spec and streams a canned output.
type Fake struct {
	Output   string
	Err      error
	LastSpec Spec
}

var _ Runtime = (*Fake)(nil)

type readerProcess struct{ r io.Reader }

func (p readerProcess) Stdout() io.Reader { return p.r }
func (p readerProcess) Wait() error       { return nil }

// Start records the spec and returns a process streaming the canned Output.
func (f *Fake) Start(_ context.Context, spec Spec) (Process, error) {
	f.LastSpec = spec
	if f.Err != nil {
		return nil, f.Err
	}
	return readerProcess{r: strings.NewReader(f.Output)}, nil
}
