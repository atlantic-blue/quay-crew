package headroom_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/headroom"
)

// countingSource records how often anything read the machine, which is what rule one of issue 405
// is about: the expensive read must not sit on the path the header redraws every second.
type countingSource struct {
	mu     sync.Mutex
	calls  int
	sample headroom.Sample
	err    error
}

func (c *countingSource) Sample(context.Context) (headroom.Sample, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.sample, c.err
}

func (c *countingSource) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// Rule one. Reading the last sample never reads the machine, however often anybody asks.
func TestReadingTheLastSampleNeverReachesTheMachine(t *testing.T) {
	source := &countingSource{sample: headroom.Sample{
		Used: headroom.Measured(100 << 20), Limit: headroom.Measured(1000 << 20), TakenAt: time.Now(),
	}}
	sampler := headroom.NewSampler(source, time.Hour)
	sampler.Once(context.Background())
	if source.count() != 1 {
		t.Fatalf("one sample read the machine %d times", source.count())
	}

	// A header redraws every second. This is a minute of them.
	for i := 0; i < 60; i++ {
		if got := sampler.Latest().Used.String(); got != "100 MiB" {
			t.Fatalf("the last sample reads %q", got)
		}
	}
	if source.count() != 1 {
		t.Fatalf("sixty reads of the last sample reached the machine %d times, want once", source.count())
	}
}

// A crew that has not sampled yet says unknown rather than nothing at all, and it never says room.
func TestASamplerNobodyHasRunSaysUnknown(t *testing.T) {
	sampler := headroom.NewSampler(&countingSource{}, time.Hour)
	latest := sampler.Latest()
	if latest.Taken() {
		t.Fatal("a sampler that has never run says it took a sample")
	}
	if latest.State() != headroom.StateUnknown {
		t.Fatalf("it says %q", latest.State())
	}
}

// A crew with no source is a crew with no daemon to ask. It reports unknown for ever and never
// refuses to start.
func TestACrewWithNothingToReadTheMachineWithReportsUnknown(t *testing.T) {
	sampler := headroom.NewSampler(nil, time.Hour)
	ctx, stop := context.WithCancel(context.Background())
	stop()
	sampler.Run(ctx)
	sampler.Once(context.Background())
	if sampler.Latest().State() != headroom.StateUnknown {
		t.Fatalf("a crew with no source says %q", sampler.Latest().State())
	}
	if sampler.Taken() != 0 {
		t.Fatalf("it took %d samples with nothing to sample", sampler.Taken())
	}
}

// Rule four. A source that fails leaves the figures unknown, keeps the reason, and never panics or
// blocks. Nothing that reads this is allowed to fail because of it.
func TestASourceThatFailsLeavesUnknownAndSaysWhy(t *testing.T) {
	source := &countingSource{err: fmt.Errorf("the daemon is not answering")}
	sampler := headroom.NewSampler(source, time.Hour)
	sampler.Once(context.Background())

	latest := sampler.Latest()
	if latest.Used.Known() || latest.Limit.Known() {
		t.Fatalf("a failed read left figures behind: %s of %s", latest.Used, latest.Limit)
	}
	if latest.State() != headroom.StateUnknown {
		t.Fatalf("a failed read says %q", latest.State())
	}
	if latest.Failed == "" {
		t.Fatal("a failed read says nothing about why")
	}
}

// A read that works after one that failed replaces it, so a crew recovers on its own.
func TestAReadThatWorksAfterAFailureReplacesIt(t *testing.T) {
	source := &countingSource{err: fmt.Errorf("the daemon is not answering")}
	sampler := headroom.NewSampler(source, time.Hour)
	sampler.Once(context.Background())

	source.mu.Lock()
	source.err = nil
	source.sample = headroom.Sample{
		Used: headroom.Measured(100 << 20), Limit: headroom.Measured(1000 << 20), TakenAt: time.Now(),
	}
	source.mu.Unlock()
	sampler.Once(context.Background())

	latest := sampler.Latest()
	if !latest.Used.Known() {
		t.Fatal("the crew did not recover from a daemon that came back")
	}
	if latest.Failed != "" {
		t.Fatalf("it still carries %q from the read before", latest.Failed)
	}
}

// The timer is the sampler's own. Run takes one immediately, because a crew that started ten seconds
// ago should not report unknown to the first operator who looks.
func TestRunTakesOneSampleImmediatelyAndThenOnItsOwnTimer(t *testing.T) {
	source := &countingSource{sample: headroom.Sample{
		Used: headroom.Measured(1), Limit: headroom.Measured(2), TakenAt: time.Now(),
	}}
	sampler := headroom.NewSampler(source, 10*time.Millisecond)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go sampler.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && sampler.Taken() < 3 {
		time.Sleep(2 * time.Millisecond)
	}
	if sampler.Taken() < 3 {
		t.Fatalf("the sampler took %d samples on its own timer", sampler.Taken())
	}
	stop()

	// And it stops when its context does, rather than reading the machine for ever.
	settled := sampler.Taken()
	time.Sleep(50 * time.Millisecond)
	if grew := sampler.Taken() - settled; grew > 1 {
		t.Fatalf("the sampler took %d more samples after it was stopped", grew)
	}
}
