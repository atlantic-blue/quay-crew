package controlplane

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// healthStream is where a health probe writes its record. It is a stream of the crew rather than of
// a workspace, because the question is about the crew and no workspace owns it.
const healthStream = "health"

// healthWorkspace namespaces that stream. A workspace of this name would share the topic, which
// costs nothing: the record carries no workspace and nothing consumes it.
const healthWorkspace = "crew"

// probeEvery is the floor under how stale the reading a view reads may be.
//
// It matches the interval the compose stack's health check runs at, because that check is what keeps
// the reading fresh wherever there is one. The timer only probes when nothing else has, so a crew
// under a health check writes what it always wrote, and a crew nobody checks still knows what it is.
const probeEvery = 30 * time.Second

// ComponentHealth is one part of the crew and what the last probe of it found.
type ComponentHealth struct {
	Name  string
	State string
	// Detail is why it is down, and empty when there is nothing wrong to say.
	Detail string
}

// HealthReading is the crew's last probe of everything it has to write to before a dispatch starts.
//
// It is remembered rather than taken on demand. A broker that is down costs the whole export budget
// on every write, so a view that probed when it drew would stall for as long as the thing it reports
// on stays broken, and the operator would read a hung console instead of a dead event log.
type HealthReading struct {
	Components []ComponentHealth
	// TakenAt is when the probe ran. The zero time is a crew that has never probed.
	TakenAt time.Time
}

// Taken says whether anything probed this.
func (r HealthReading) Taken() bool { return !r.TakenAt.IsZero() }

// Failure is what did not land, or nil when every part answered. It is what the log line says, so
// whoever is diagnosing a crew reads which write failed rather than that one did.
func (r HealthReading) Failure() error {
	var failures []error
	for _, component := range r.Components {
		if component.State == display.HealthDown {
			failures = append(failures, errors.New(component.Detail))
		}
	}
	return errors.Join(failures...)
}

// Health says whether this crew can start job, which is not the same question as whether its
// process is up.
//
// A control plane served every listing in under a second and dispatched nothing for an hour. Every
// read answered, so every view of the crew looked well, and the operator read the dispatches as a
// slow model. A check that only reads would have agreed with them. So this one writes: the store
// takes a row, and the event log takes a record, which are the two writes every dispatch makes
// before a sandbox is ever asked for. See issue 400.
type Health struct {
	server *Server
}

var _ grpc_health_v1.HealthServer = (*Health)(nil)

// NewHealth answers for the crew this server is.
func NewHealth(server *Server) *Health {
	return &Health{server: server}
}

// Check writes, and says not serving when a write does not land inside the budget a dispatch gives
// it. The reason goes to the log: the answer itself carries only the verdict, so it is said where
// somebody diagnosing this will look. The reading it took is kept, and GetHealth is where the reason
// is readable from a console rather than from a container's log.
func (h *Health) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if err := h.server.ProbeHealth(ctx).Failure(); err != nil {
		slog.WarnContext(ctx, "this crew is not serving", "error", err)
		return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_NOT_SERVING}, nil
	}
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

// List answers for the one service this process serves.
func (h *Health) List(ctx context.Context, _ *grpc_health_v1.HealthListRequest) (*grpc_health_v1.HealthListResponse, error) {
	checked, err := h.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return nil, err
	}
	return &grpc_health_v1.HealthListResponse{
		Statuses: map[string]*grpc_health_v1.HealthCheckResponse{
			quaycrewv1.ControlPlaneService_ServiceDesc.ServiceName: checked,
		},
	}, nil
}

// Watch is not served. A caller that wants to know now asks now.
func (h *Health) Watch(_ *grpc_health_v1.HealthCheckRequest, _ grpc_health_v1.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "this crew answers a health check when it is asked")
}

// GetHealth is the crew's last probe, one entry per part.
//
// It reads the remembered reading and never probes, the way GetHeadroom reads the last sample and
// never the daemon. A view draws on a timer, and a part that is down is exactly the part that takes
// the longest to say so.
func (s *Server) GetHealth(_ context.Context, _ *quaycrewv1.GetHealthRequest) (*quaycrewv1.GetHealthResponse, error) {
	reading := s.LastHealth()
	resp := &quaycrewv1.GetHealthResponse{}
	if reading.Taken() {
		resp.CheckedAt = timestamppb.New(reading.TakenAt)
	}
	for _, component := range reading.Components {
		resp.Components = append(resp.Components, &quaycrewv1.HealthComponent{
			Name:   component.Name,
			State:  component.State,
			Detail: component.Detail,
		})
	}
	return resp, nil
}

// LastHealth is the reading whoever probed last took. A crew that has never probed answers a reading
// with nothing in it, and a caller must say so rather than filling the gap: a part nobody checked
// must never read the same as a part that answered.
func (s *Server) LastHealth() HealthReading {
	s.healthMu.RLock()
	defer s.healthMu.RUnlock()
	return s.lastHealth
}

// ProbeHealth writes where a dispatch writes, keeps what it found, and returns it. The health check
// calls it, and so does a test that cannot wait for a timer.
func (s *Server) ProbeHealth(ctx context.Context) HealthReading {
	// The export budget rather than the start budget: a write a dispatch would wait a minute for is
	// still one a health check must not wait a minute for, because whoever asked re-asks.
	ctx, giveUp := context.WithTimeout(ctx, s.exportWait)
	defer giveUp()

	reading := HealthReading{
		Components: []ComponentHealth{s.probeStore(ctx), s.probeEvents(ctx)},
		TakenAt:    time.Now(),
	}
	s.healthMu.Lock()
	s.lastHealth = reading
	s.healthMu.Unlock()
	return reading
}

// RunHealth keeps the reading fresh until the context ends. Whoever owns the process starts it, the
// way the headroom sampler is started, because a goroutine hidden inside a constructor is a lifetime
// nobody can see.
//
// It probes only when nothing else has inside the interval. The compose stack's health check already
// writes every thirty seconds, and a second timer beside it would double what a crew writes to say
// the same thing twice.
func (s *Server) RunHealth(ctx context.Context) {
	ticker := time.NewTicker(probeEvery)
	defer ticker.Stop()
	// Once at the start, so the first operator to open a view of a crew that came up a moment ago
	// reads what it found rather than that it has not looked.
	s.ProbeHealth(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(s.LastHealth().TakenAt) < probeEvery {
				continue
			}
			s.ProbeHealth(ctx)
		}
	}
}

// probeStore takes a write on the store, which is the first thing a dispatch does.
func (s *Server) probeStore(ctx context.Context) ComponentHealth {
	if err := waited(ctx, "", waitStoreWrite, s.store.Probe); err != nil {
		return ComponentHealth{
			Name:   display.HealthStore,
			State:  display.HealthDown,
			Detail: fmt.Sprintf("the store did not take a write: %v", err),
		}
	}
	return ComponentHealth{Name: display.HealthStore, State: display.HealthServing}
}

// probeEvents puts a record on the event log, which is the second.
//
// A crew with no log configured is not serving and not down: it is a crew whose tasks are recorded
// nowhere, and it says that. The redpanda that died on 29 August 2026 was configured and answering
// nothing, and the two must not read alike.
func (s *Server) probeEvents(ctx context.Context) ComponentHealth {
	if !s.hasEventLog() {
		return ComponentHealth{Name: display.HealthEvents, State: display.HealthNotConfigured}
	}
	topic, err := messaging.Topic(healthWorkspace, healthStream)
	if err != nil {
		return ComponentHealth{
			Name:   display.HealthEvents,
			State:  display.HealthDown,
			Detail: fmt.Sprintf("the health stream has no topic: %v", err),
		}
	}
	if err := s.export(ctx, "", topic, []byte(quaycrewv1.ControlPlaneService_ServiceDesc.ServiceName)); err != nil {
		return ComponentHealth{
			Name:   display.HealthEvents,
			State:  display.HealthDown,
			Detail: fmt.Sprintf("the event log did not take a record: %v", err),
		}
	}
	return ComponentHealth{Name: display.HealthEvents, State: display.HealthServing}
}

// hasEventLog says whether anything is connected to the log. A crew configured with none is given a
// log that discards, so every write to it succeeds, and a probe that read that as an answer would
// report a crew recording nothing as a crew in good health.
func (s *Server) hasEventLog() bool {
	_, discards := s.events.(messaging.Discard)
	return !discards
}
