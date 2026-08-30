package telemetry

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

// ServerOptions returns what a gRPC server needs so that every inbound message runs inside a span.
//
// This is the thing that was missing: Init built a tracer provider and an exporter, and nothing ever
// started a span, so the exporter had nothing to send and every signal downstream of it was empty.
//
// It is a stats handler rather than an interceptor because a stats handler runs before the
// interceptor chain. A call refused by the system's token guard is therefore traced too, which is the
// call somebody is most likely to come looking for.
func ServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{grpc.StatsHandler(otelgrpc.NewServerHandler())}
}
