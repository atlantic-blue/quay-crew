// Command controlplane is the spine service: it serves the ControlPlaneService gRPC API. The model
// backend and the sandbox provider are chosen by configuration, so the control plane depends only on
// the interfaces, not on any concrete implementation. The default model backend is the Claude Code
// adapter (your subscription, no API cost); each session runs in its own sandbox (Docker by default).
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
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/telemetry"
	"google.golang.org/grpc"
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

	runner, err := model.NewRunner(os.Getenv("QC_MODEL"), os.Getenv("QC_WORKDIR"))
	if err != nil {
		logger.Error("model runner config failed", "error", err)
		os.Exit(1)
	}

	provider, err := sandbox.NewProvider(
		os.Getenv("QC_SANDBOX"),
		os.Getenv("QC_SANDBOX_IMAGE"),
		splitAndTrim(os.Getenv("QC_SANDBOX_MOUNTS")),
	)
	if err != nil {
		logger.Error("sandbox provider config failed", "error", err)
		os.Exit(1)
	}

	durable, err := openStore(ctx, os.Getenv("QC_DATABASE_URL"), logger)
	if err != nil {
		logger.Error("store open failed", "error", err)
		os.Exit(1)
	}
	defer durable.Close()

	server := controlplane.NewServer(durable, runner, provider, secrets.NewMemory())
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

	<-ctx.Done()
	logger.Info("shutting down")
	grpcServer.GracefulStop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}

// openStore returns the durable store. With QC_DATABASE_URL set it is Postgres, and the migrations
// are applied on the way up. Without it the store is in memory, which loses every project and
// session on restart and is only appropriate for a throwaway stack.
func openStore(ctx context.Context, databaseURL string, logger *slog.Logger) (store.Store, error) {
	if databaseURL == "" {
		logger.Warn("no QC_DATABASE_URL set, using the in memory store: projects and sessions will not survive a restart")
		return store.NewMemory(), nil
	}
	durable, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	logger.Info("store ready", "backend", "postgres")
	return durable, nil
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
