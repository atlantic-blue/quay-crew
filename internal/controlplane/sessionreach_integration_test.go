//go:build integration

package controlplane_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc"
)

// A session calling the system, against a real daemon: a real container, on the network the system's
// own provider put it on, running the real command line tool, over a real gRPC interface with the
// interceptor and the deny policy in front of it.
//
// None of it can be proved with a double. The fault was that nothing arrived, and a double answers
// whatever it is told to: it would have reported this feature working for as long as it existed.
//
// What is substituted is the model, which is what continuous integration substitutes everywhere
// else. The runner here runs the exec's text as a shell command inside the session's sandbox, with
// the environment the system built for that exec, which is exactly what the Claude Code adapter does
// with the same values.
//
// What is not proved here: the control plane in this test listens on the host rather than in a
// container, so a session dials it by address rather than by name.

// A session is on a network of its own, and the system's store is not on it. The session can reach the
// system and nothing else, which is what keeps a session that can drive the system from reading every
// workspace's rows straight out of the database.
//
// Both halves are asserted, because either alone can pass for the wrong reason: the name does not
// resolve, and the address it would resolve to accepts nothing. And the same command against the
// system's own address has to succeed, or this test proves only that the command is broken.
func TestASessionCannotOpenAConnectionToPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	system := aSystemWhoseSessionsCanReachIt(ctx, t)

	where := system.postgresOnTheSystemsNetwork(ctx, t)

	said := system.run(ctx, t, connects+strings.Join([]string{
		`echo "by name: $(getent hosts postgres || echo unresolved)"`,
		`echo "by address: $(reach ` + where + `:5432)"`,
		`echo "the system: $(reach "$QC_GRPC_ADDR")"`,
	}, "\n"))

	if !strings.Contains(said, "by name: unresolved") {
		t.Errorf("a session resolves the name of the system's store:\n%s", said)
	}
	if !strings.Contains(said, "by address: refused") {
		t.Errorf("a session opened a connection to the system's store:\n%s", said)
	}
	if !strings.Contains(said, "the system: open") {
		t.Fatalf("a session cannot open a connection to the system either, so the two checks above prove nothing:\n%s", said)
	}
}

// connects declares one attempt at a plain connection, answering open or refused. One shape for the
// store and for the system, so a refusal is the network's answer rather than a difference between two
// commands.
const connects = `reach() {
  local at="$1"
  timeout 5 bash -c "exec 3<>/dev/tcp/${at%:*}/${at##*:}" >/dev/null 2>&1 && echo open || echo refused
}
`

// reachableSystem is a control plane serving on the host, with its sessions on a network of their own.
type reachableSystem struct {
	server         *controlplane.Server
	projectID      string
	workspaceID    string
	sessionNetwork string
	systemNetwork  string
}

// aSystemWhoseSessionsCanReachIt builds the whole thing: two networks, a control plane serving on the
// host with the real interceptor in front of it, and a workspace with a project in it.
func aSystemWhoseSessionsCanReachIt(ctx context.Context, t *testing.T) *reachableSystem {
	t.Helper()
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the sandbox image, which is the one carrying krewe")
	}

	systemNetwork := aNetwork(t, "system")
	sessionNetwork := aNetwork(t, "sessions")

	// A container reaches the host at the gateway of the network it is on. In the composed stack the
	// control plane is a container on this network and a session dials it by name; here it is this
	// test process, so the address is the gateway and the port it took.
	gateway := networkGateway(t, sessionNetwork)
	listener, port := aListener(t)

	server := controlplane.NewServer(controlplane.Config{
		Store:    store.NewMemory(),
		Runner:   &shellRunner{},
		Secrets:  secrets.NewMemory(),
		Provider: sandbox.DockerProvider{Image: image, SessionNetwork: sessionNetwork},
		// Where a session dials the system, put into the container as QC_GRPC_ADDR.
		Reachable:   fmt.Sprintf("%s:%d", gateway, port),
		DriverToken: "the-driver's-token",
	})

	// The guard the composed system runs behind, and the reason this is worth doing over a container:
	// the refusal has to come from the interceptor and the policy rather than from a test.
	grpcServer := grpc.NewServer(auth.ServerOptions(auth.Policy{
		Token:       "the-operator's-token",
		DriverToken: "the-driver's-token",
		Denied:      controlplane.DeniedToDriver,
	})...)
	quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	system := &reachableSystem{server: server, sessionNetwork: sessionNetwork, systemNetwork: systemNetwork}

	// Built through the server's own methods rather than over the wire, which is what the other
	// integration tests in this package do: the operator's half is not what is under test.
	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	system.workspaceID = workspace.GetWorkspace().GetId()
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: system.workspaceID, Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	system.projectID = project.GetProject().GetId()

	return system
}

// run dispatches an exec to the driver and returns what it said.
//
// The driver rather than an ordinary session, because the driver is the one session the system tells
// where it is: an ordinary session is told no address and no token at all, which is what
// features/sessions.feature specifies. So the driver is the session that can reach the system, and
// therefore the only one where "it reaches the system and nothing else" is a claim worth making.
//
// The script runs inside that session's container, in the environment the system built for the exec,
// which is where the address and the token are.
func (c *reachableSystem) run(ctx context.Context, t *testing.T, script string) string {
	t.Helper()
	opened, err := c.server.OpenDriver(ctx, &quaycrewv1.OpenDriverRequest{Project: c.projectID})
	if err != nil {
		t.Fatalf("open the driver: %v", err)
	}
	dispatched, err := c.server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: c.projectID, Handle: opened.GetSession().GetHandle(), Text: script,
	})
	if err != nil {
		t.Fatalf("dispatch the exec: %v", err)
	}
	c.removeSandbox(t, dispatched.GetId())
	return dispatched.GetReply()
}

func (c *reachableSystem) postgresOnTheSystemsNetwork(ctx context.Context, t *testing.T) string {
	t.Helper()
	name := "quaycrew-itest-postgres-" + store.NewID()
	out, err := exec.CommandContext(ctx, "docker", "run", "--detach", "--name", name,
		"--network", c.systemNetwork, "--network-alias", "postgres",
		"--env", "POSTGRES_PASSWORD=quaycrew", "postgres:17-alpine").CombinedOutput()
	if err != nil {
		t.Fatalf("start the store: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	// Listening, not merely created: a store that has not opened its port yet would refuse a
	// connection for a reason that has nothing to do with the network.
	for range 60 {
		if err := exec.Command("docker", "exec", name, "pg_isready", "-U", "postgres").Run(); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	address, err := exec.CommandContext(ctx, "docker", "inspect", "--format",
		"{{(index .NetworkSettings.Networks \""+c.systemNetwork+"\").IPAddress}}", name).Output()
	if err != nil {
		t.Fatalf("ask the daemon where the store is: %v", err)
	}
	return strings.TrimSpace(string(address))
}

// removeSandbox takes a session's container back at the end of the test, whatever happened to it.
func (c *reachableSystem) removeSandbox(t *testing.T, sessionID string) {
	t.Helper()
	if sessionID == "" {
		return
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", sandbox.ContainerName(sessionID)).Run() })
}

// aNetwork creates one network for this test and removes it afterwards.
func aNetwork(t *testing.T, what string) string {
	t.Helper()
	name := "quaycrew-itest-" + what + "-" + store.NewID()
	if out, err := exec.Command("docker", "network", "create", name).CombinedOutput(); err != nil {
		t.Fatalf("create the %s network: %v: %s", what, err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "network", "rm", name).Run() })
	return name
}

// aListener is where this system serves, on every address of the host, because what has to reach it is
// a container rather than this process. The port is the daemon's to choose so two runs of this suite
// do not collide.
func aListener(t *testing.T) (net.Listener, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener, listener.Addr().(*net.TCPAddr).Port
}

// networkGateway is the address a container on this network reaches the host at.
func networkGateway(t *testing.T, network string) string {
	t.Helper()
	out, err := exec.Command("docker", "network", "inspect", "--format",
		"{{range .IPAM.Config}}{{.Gateway}}{{end}}", network).Output()
	if err != nil {
		t.Fatalf("ask the daemon for the gateway of %s: %v", network, err)
	}
	gateway := strings.TrimSpace(string(out))
	if gateway == "" {
		t.Fatalf("the network %s has no gateway, so nothing in it can reach this process", network)
	}
	return gateway
}

// shellRunner runs the exec's text as a command inside the session's sandbox, with the environment
// the system built for that exec.
//
// It stands in for the model and for nothing else. The Claude Code adapter execs in the same way with
// the same values, which is what makes this a proof about the system rather than about the double: the
// address and the credential reach the container the same way either runner is in front of them.
type shellRunner struct{}

var _ model.Runner = (*shellRunner)(nil)

func (r *shellRunner) Run(ctx context.Context, box sandbox.Sandbox, req model.Request) (model.Response, error) {
	if box == nil {
		return model.Response{}, fmt.Errorf("model: no sandbox provided")
	}
	env := make([]string, 0, len(req.Env))
	for key, value := range req.Env {
		env = append(env, key+"="+value)
	}
	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"bash", "-c", req.Text}, Env: env})
	if err != nil {
		return model.Response{}, fmt.Errorf("model: exec: %w", err)
	}
	said, readErr := io.ReadAll(proc.Stdout())
	// The exit status is deliberately not an error. A refusal is what these tests are about, and
	// `krewe` exits non zero when the system refuses it, so a runner that failed the exec would throw
	// away the sentence being asserted on.
	_ = proc.Wait()
	if readErr != nil {
		return model.Response{}, fmt.Errorf("model: read what the session said: %w", readErr)
	}
	return model.Response{Reply: string(said), ModelSessionID: "shell"}, nil
}
