package controlplane

import (
	"context"
	"fmt"
	"log/slog"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// healthStream is where a health probe writes its record. It is a stream of the crew rather than of
// a workspace, because the question is about the crew and no workspace owns it.
const healthStream = "health"

// healthWorkspace namespaces that stream. A workspace of this name would share the topic, which
// costs nothing: the record carries no workspace and nothing consumes it.
const healthWorkspace = "crew"

// Health says whether this crew can start work, which is not the same question as whether its
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
// somebody diagnosing this will look.
func (h *Health) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if err := h.probe(ctx); err != nil {
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

// probe writes where a dispatch writes, under the budget a dispatch gets, and names which write did
// not land.
func (h *Health) probe(ctx context.Context) error {
	// The export budget rather than the start budget: a write a dispatch would wait a minute for is
	// still one a health check must not wait a minute for, because whoever asked re-asks.
	ctx, giveUp := context.WithTimeout(ctx, h.server.exportWait)
	defer giveUp()

	if err := waited(ctx, "", waitStoreWrite, h.server.store.Probe); err != nil {
		return fmt.Errorf("the store did not take a write: %w", err)
	}

	topic, err := messaging.Topic(healthWorkspace, healthStream)
	if err != nil {
		return fmt.Errorf("the health stream has no topic: %w", err)
	}
	if err := h.server.export(ctx, "", topic, []byte(quaycrewv1.ControlPlaneService_ServiceDesc.ServiceName)); err != nil {
		return fmt.Errorf("the event log did not take a record: %w", err)
	}
	return nil
}
