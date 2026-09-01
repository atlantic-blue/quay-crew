// Command gateway is a Quay System service skeleton. It boots telemetry and structured logging,
// then waits for a shutdown signal. The real gateway wiring (channels, event log, control plane)
// lands in later milestones; this proves the service shape.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/logging"
	"github.com/atlantic-blue/quay-krewe/internal/telemetry"
)

func main() {
	serviceName := envOr("QC_SERVICE_NAME", "gateway")
	logger := logging.Init(serviceName, os.Stdout)

	otelEndpoint := envOr("QC_OTEL_ENDPOINT", "localhost:4317")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.Init(ctx, serviceName, otelEndpoint)
	if err != nil {
		logger.Error("telemetry init failed", "error", err)
		os.Exit(1)
	}
	// Every line goes to the collector as well as to stdout, now that there is a pipeline to take
	// it. Stdout keeps carrying all of it: it is the signal that still works when the collector does
	// not.
	logger = logging.AlsoExport(serviceName, os.Stdout)
	logger.Info("service started", "otel_endpoint", otelEndpoint)

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
