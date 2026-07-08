// Command controlplane is the spine service: it serves the ControlPlaneService gRPC API and, when a
// Kafka seed is configured, consumes inbound channel messages from the event log and routes them to
// sessions. The model runner defaults to the Claude Code adapter (your subscription, no API cost).
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	serviceName := envOr("QC_SERVICE_NAME", "controlplane")
	otelEndpoint := envOr("QC_OTEL_ENDPOINT", "localhost:4317")
	grpcAddr := envOr("QC_GRPC_ADDR", ":50051")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.Init(ctx, serviceName, otelEndpoint)
	if err != nil {
		logger.Error("telemetry init failed", "error", err)
		os.Exit(1)
	}

	server := controlplane.NewServer(model.NewClaudeCodeRunner(), secrets.NewMemory())

	grpcServer := grpc.NewServer()
	quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, server)

	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		logger.Error("listen failed", "addr", grpcAddr, "error", err)
		os.Exit(1)
	}

	go func() {
		logger.Info("control plane serving", "grpc", grpcAddr, "otel_endpoint", otelEndpoint)
		if err := grpcServer.Serve(listener); err != nil {
			logger.Error("grpc serve stopped", "error", err)
		}
	}()

	// Optional event log consumer: route inbound channel messages to sessions.
	log := startEventLogConsumer(ctx, logger, server)

	<-ctx.Done()
	logger.Info("shutting down")
	grpcServer.GracefulStop()
	if log != nil {
		log.Close()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}

// startEventLogConsumer wires the event log to the control plane when QC_KAFKA_SEEDS and
// QC_INBOUND_TOPICS are set. It returns the event log so the caller can close it.
func startEventLogConsumer(ctx context.Context, logger *slog.Logger, server *controlplane.Server) messaging.EventLog {
	seeds := splitAndTrim(os.Getenv("QC_KAFKA_SEEDS"))
	topics := splitAndTrim(os.Getenv("QC_INBOUND_TOPICS"))
	if len(seeds) == 0 || len(topics) == 0 {
		logger.Info("event log consumer disabled (set QC_KAFKA_SEEDS and QC_INBOUND_TOPICS to enable)")
		return nil
	}

	eventLog, err := messaging.NewClient(seeds...)
	if err != nil {
		logger.Error("event log client failed", "error", err)
		return nil
	}

	handler := func(ctx context.Context, r messaging.Record) error {
		var msg quaycrewv1.InboundMessage
		if err := proto.Unmarshal(r.Value, &msg); err != nil {
			logger.Error("bad inbound message, skipping", "topic", r.Topic, "error", err)
			return nil
		}
		if err := server.HandleInbound(ctx, &msg); err != nil {
			logger.Error("handle inbound failed", "project", msg.GetProject(), "error", err)
		}
		return nil
	}

	go func() {
		logger.Info("consuming inbound", "topics", topics)
		if err := eventLog.Consume(ctx, "controlplane", topics, handler); err != nil && ctx.Err() == nil {
			logger.Error("consumer stopped", "error", err)
		}
	}()
	return eventLog
}

func splitAndTrim(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
