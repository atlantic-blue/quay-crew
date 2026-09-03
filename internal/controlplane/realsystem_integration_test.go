//go:build integration

package controlplane_test

import (
	"context"
	"net"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/console"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// aRealStore is a Postgres of this test's own, migrated.
func aRealStore(t *testing.T, ctx context.Context) *store.Postgres {
	t.Helper()
	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("quaycrew"),
		postgres.WithUsername("quaycrew"),
		postgres.WithPassword("quaycrew"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		timeout := 30 * time.Second
		_ = container.Stop(context.Background(), &timeout)
	})
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	durable, err := store.NewPostgres(ctx, url)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(durable.Close)
	return durable
}

// servedOver puts the system behind the real gRPC interface, which is the one every caller speaks.
func servedOver(t *testing.T, server *controlplane.Server) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return quaycrewv1.NewControlPlaneServiceClient(conn)
}

// rowFor is the listed row for one thing, found by identifier rather than by position, so the case is
// about what the row says and not about the order the store answers in.
func rowFor(rows []console.Row, id string) (console.Row, bool) {
	for _, row := range rows {
		if row.ID == id {
			return row, true
		}
	}
	return console.Row{}, false
}

// cellAt is where a column sits in a row, read off the view rather than written here. A number in a
// test goes stale the moment somebody adds a column, and the row then reads one cell to the left of
// the heading that names it.
func cellAt(t *testing.T, resource console.Resource, title string) int {
	t.Helper()
	for at, column := range resource.Columns {
		if column.Title == title {
			return at
		}
	}
	t.Fatalf("the %s view has no %q column: %v", resource.Name, title, resource.Columns)
	return 0
}
