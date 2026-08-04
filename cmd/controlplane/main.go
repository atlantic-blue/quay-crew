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
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/projection"
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
	// The kinds are resolved through the same functions the constructors use, so what the control
	// plane says it is running cannot drift from what it built.
	modelKind, _ := model.ResolveKind(os.Getenv("QC_MODEL"))

	// QC_DATA_DIR is where this process writes a workspace's conversation store and a project's
	// files; QC_DATA_HOST is the same directory as the host daemon sees it, which is what a sandbox
	// actually mounts. In a container the two differ, so both are needed; run this on the host and
	// they are the same path. Neither set means state stays in the container and dies with it.
	storage := sandbox.Storage{Dir: os.Getenv("QC_DATA_DIR"), Host: os.Getenv("QC_DATA_HOST")}
	if storage.Dir == "" {
		logger.Warn("no QC_DATA_DIR set: a session's conversation lives inside its container and is destroyed with it")
	}

	provider, err := sandbox.NewProvider(os.Getenv("QC_SANDBOX"), sandbox.Options{
		Image:   os.Getenv("QC_SANDBOX_IMAGE"),
		Mounts:  splitAndTrim(os.Getenv("QC_SANDBOX_MOUNTS")),
		Storage: storage,
	})
	if err != nil {
		logger.Error("sandbox provider config failed", "error", err)
		os.Exit(1)
	}
	sandboxKind, _ := sandbox.ResolveKind(os.Getenv("QC_SANDBOX"))

	durable, storeKind, err := openStore(ctx, os.Getenv("QC_DATABASE_URL"), logger)
	if err != nil {
		logger.Error("store open failed", "error", err)
		os.Exit(1)
	}
	defer durable.Close()

	events, eventsKind := openEventLog(os.Getenv("QC_KAFKA_SEEDS"), logger)
	defer events.Close()

	server := controlplane.NewServer(controlplane.Config{
		Store:    durable,
		Runner:   runner,
		Provider: provider,
		Secrets:  secrets.NewMemory(),
		Storage:  storage,
		Events:   events,
		Info: controlplane.Info{
			Model:   modelKind,
			Sandbox: sandboxKind,
			Store:   storeKind,
			State:   stateKind(storage),
			Events:  eventsKind,
		},
	})
	// The projection reads the log back into the store, so a session's history can be listed without
	// replaying the log on every request. It runs here rather than as its own service because it
	// materialises into the store this process already owns; when it needs to scale separately, it
	// moves out behind the same interfaces.
	if eventsKind != "" {
		go func() {
			if err := projection.New(events, durable, logger).Run(ctx); err != nil && ctx.Err() == nil {
				logger.Error("projection stopped, so session history will go stale until a restart", "error", err)
			}
		}()
	}

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

// openEventLog returns the log turns are published to. With QC_KAFKA_SEEDS set it is Kafka, spoken
// to Redpanda locally. Without it, turns run and nothing records that they did, which the status
// block says out loud rather than leaving an empty column to read as fine.
//
// The producer connects lazily, so a broker that is not up yet does not stop the control plane from
// serving. A publish that cannot reach it is dropped rather than failing the turn.
func openEventLog(seeds string, logger *slog.Logger) (messaging.EventLog, string) {
	brokers := splitAndTrim(seeds)
	if len(brokers) == 0 {
		logger.Warn("no QC_KAFKA_SEEDS set: turns will run without being recorded on the event log")
		return messaging.Discard{}, ""
	}
	client, err := messaging.NewClient(brokers...)
	if err != nil {
		logger.Warn("event log unavailable, turns will run without being recorded", "error", err)
		return messaging.Discard{}, ""
	}
	logger.Info("event log ready", "backend", "kafka", "seeds", brokers)
	return client, "kafka"
}

// openStore returns the durable store. With QC_DATABASE_URL set it is Postgres, and the migrations
// are applied on the way up. Without it the store is in memory, which loses every workspace and
// session on restart and is only appropriate for a throwaway stack.
func openStore(ctx context.Context, databaseURL string, logger *slog.Logger) (store.Store, string, error) {
	if databaseURL == "" {
		logger.Warn("no QC_DATABASE_URL set, using the in memory store: workspaces and sessions will not survive a restart")
		return store.NewMemory(), "memory", nil
	}
	durable, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		return nil, "", err
	}
	logger.Info("store ready", "backend", "postgres")
	return durable, "postgres", nil
}

// stateKind names where a session's conversation and files are kept, for the status block. It is
// deliberately not a promise that any particular thread's sandbox has the mounts: a sandbox created
// before they were configured does not, which is why an upgrade clears the old ones.
func stateKind(storage sandbox.Storage) string {
	if storage.Dir == "" {
		return ""
	}
	return "host directory " + storage.Host
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
