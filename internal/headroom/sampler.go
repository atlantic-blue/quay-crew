package headroom

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Every is how often the system reads the machine.
//
// It is a timer of its own because the header redraws every second and reading the daemon is not a
// once a second cost: `docker stats --no-stream` waits for the daemon to sample every container. So
// the header reads the last sample and never the daemon, which is rule one of issue 405.
//
// Ten seconds is what the header can be behind by. It is provisional: the measurement that sets it
// is how long a sample takes on a machine holding twenty sandboxes, which nothing has measured yet.
const Every = 10 * time.Second

// A Sampler keeps the last sample and takes the next one on its own timer.
//
// Everything that reports headroom reads Latest. Nothing else calls the source, so a slow daemon
// slows the sampler and never a command.
type Sampler struct {
	source Source
	every  time.Duration

	mu     sync.RWMutex
	latest Sample
	// taken counts the samples that landed, so a test can say the timer ran rather than infer it
	// from a figure that might have come from anywhere.
	taken int
}

// NewSampler builds one over a source. A nil source is a system with no daemon to read, and it reports
// unknown for ever rather than refusing to start.
func NewSampler(source Source, every time.Duration) *Sampler {
	if every <= 0 {
		every = Every
	}
	return &Sampler{source: source, every: every, latest: Sample{Used: Unknown(), Limit: Unknown()}}
}

// Latest is the last sample that landed. It never blocks on the daemon, and it is the only call the
// header path makes.
func (s *Sampler) Latest() Sample {
	if s == nil {
		return Sample{Used: Unknown(), Limit: Unknown()}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

// Taken is how many samples have landed, for a test that has to prove the timer ran.
func (s *Sampler) Taken() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.taken
}

// Run samples until the context ends. It takes one sample immediately, because a system that started
// ten seconds ago should not report unknown to the first operator who looks.
//
// A source that fails never stops the loop and never fails a command. The failure is kept on the
// sample, so the operator reads why the figure is unknown rather than reading a zero.
func (s *Sampler) Run(ctx context.Context) {
	if s == nil || s.source == nil {
		return
	}
	s.Once(ctx)
	ticker := time.NewTicker(s.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Once(ctx)
		}
	}
}

// Once takes one sample now and keeps it. Run calls it on every tick, and a caller that cannot wait
// for a tick calls it directly: a test that waited a tick would be a test with a clock in it.
func (s *Sampler) Once(ctx context.Context) {
	if s == nil || s.source == nil {
		return
	}
	s.take(ctx)
}

// take takes one sample and keeps it.
func (s *Sampler) take(ctx context.Context) {
	sample, err := s.source.Sample(ctx)
	if err != nil {
		slog.WarnContext(ctx, "the machine could not be read, so the system reports unknown headroom",
			"error", err)
		// The failure is kept where a reader will find it, and the figures stay unknown. Nothing
		// here estimates: an operator stops a session on these numbers.
		s.keep(Sample{Used: Unknown(), Limit: Unknown(), Failed: err.Error()})
		return
	}
	s.keep(sample)
}

func (s *Sampler) keep(sample Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = sample
	s.taken++
}
