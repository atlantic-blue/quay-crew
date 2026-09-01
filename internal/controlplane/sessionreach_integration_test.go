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
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc"
)

// A job's session calling the system, against a real daemon: a real container, on the network the
// system's own provider put it on, running the real command line tool, over a real gRPC interface with
// the interceptor and the deny policy in front of it.
//
// None of it can be proved with a double. The fault was that nothing arrived, and a double answers
// whatever it is told to: it would have reported this feature working for as long as it existed.
//
// What is substituted is the model, which is what continuous integration substitutes everywhere
// else. The runner here runs the task's text as a shell command inside the session's sandbox, with
// the environment the system built for that task, which is exactly what the Claude Code adapter does
// with the same values.
//
// What is not proved here: the control plane in this test listens on the host rather than in a
// container, so a session dials it by address rather than by name. Name resolution on the session
// network is proved by the containers job, where the control plane is a real container on it.

// TestASessionIsRefusedAVerbItsRoleDoesNotCarry is the load bearing one, and it is a refusal rather
// than a call. Before a session could reach the system, the verb boundary in deny.go had never refused
// a real call, and a permission system that has never refused anything cannot be told apart from one
// that is not wired up.
func TestASessionIsRefusedAVerbItsRoleDoesNotCarry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	system := aSystemWhoseSessionsCanReachIt(ctx, t)

	declared := system.declare(ctx, t, "clear the backlog", assessorRole)
	said := system.run(ctx, t, declared, "krewe job stop "+declared+" \"I have had enough\" 2>&1")

	// The role carries job.create and job.read and not job.stop, so the system names the verb and
	// says where a verb comes from. A session that was refused has to know what to ask for.
	for _, want := range []string{role.VerbJobStop, "may not", "verbs list", "attaching it"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the session was told %q, want the system's refusal naming %q", said, want)
		}
	}
	// And the refusal is a refusal. A sentence that reads like one over job that stopped anyway is
	// worse than no boundary at all.
	if phase := system.phaseOf(ctx, t, declared); phase == job.PhaseStopped {
		t.Fatalf("the job is %q: the session was told it may not stop a job, and the job stopped", phase)
	}
}

// TestASessionDeclaresASubJobAndTheSystemRunsItInASessionOfItsOwn.
//
// The parent and the depth are asserted because both come from the credential and never from the
// caller. A session that could name its own parent could name none, start again at the top, and
// escape the depth count that is the only thing bounding recursion.
func TestASessionDeclaresASubJobAndTheSystemRunsItInASessionOfItsOwn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	system := aSystemWhoseSessionsCanReachIt(ctx, t)

	declared := system.declare(ctx, t, "clear the backlog", assessorRole)
	parentSession := system.run(ctx, t, declared, `krewe job create `+
		`--title "write the migration" --brief "add the column" --role `+implementerRole+` 2>&1`)

	children := system.children(ctx, t, declared)
	if len(children) != 1 {
		t.Fatalf("the session declared %d jobs, want 1. It said:\n%s", len(children), parentSession)
	}
	child := children[0]
	if child.GetParent() != declared {
		t.Fatalf("the sub job hangs under %q, want the job the session was running, %q", child.GetParent(), declared)
	}
	if child.GetDepth() != 1 {
		t.Fatalf("the sub job is at depth %d, want 1: depth comes from the parent, and the parent from the credential",
			child.GetDepth())
	}
	if child.GetRole() != implementerRole {
		t.Fatalf("the sub job runs as %q, want the role the session named", child.GetRole())
	}

	// One tick of the controller, rather than waiting for its ticker: a wait is slow when it passes
	// and flaky when it does not.
	system.server.TickJob(ctx)

	ran := system.jobNamed(ctx, t, child.GetId())
	if ran.GetSession() == "" {
		t.Fatalf("the sub job is %q and has no session, so nothing ran it", ran.GetPhase())
	}
	if ran.GetSession() == system.lastSession {
		t.Fatal("the sub job ran in the session that declared it, so a role is not a session of its own")
	}
	system.removeSandbox(t, ran.GetSession())

	// The container the system made for it is real, and it is the daemon that says so rather than the
	// system reporting on itself. The controller dispatches detached, so the container is waited for:
	// what it answers is the assertion, and how soon it exists is the machine's business.
	container := sandbox.ContainerName(ran.GetSession())
	if !within(ctx, func() bool {
		return exec.Command("docker", "inspect", container).Run() == nil
	}) {
		t.Fatalf("the daemon holds no container called %s, so the sub job has a session and nowhere to run",
			container)
	}
}

// TestASessionDeclaresAChildLongAfterTheFirstMinuteOfItsJob is issue 449 at the only tier that can
// prove it: a real container, holding the credential the system minted for its job, calling the system
// over the real interface with the interceptor in front of it.
//
// The session waits past the minute its credential used to last, and then declares a child. It waits
// for real. A double can be told what time it is, and the fault here was that a session still doing
// its job could no longer call the system at all, which a double would have reported as working for as
// long as it existed. The run that found it had been going for twenty nine minutes.
func TestASessionDeclaresAChildLongAfterTheFirstMinuteOfItsJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	system := aSystemWhoseSessionsCanReachIt(ctx, t)

	declared := system.declare(ctx, t, "clear the backlog", assessorRole)
	said := system.run(ctx, t, declared, "sleep 75\n"+
		`krewe job create --title "write the migration" --brief "add the column" --role `+
		implementerRole+" 2>&1")

	children := system.children(ctx, t, declared)
	if len(children) != 1 {
		t.Fatalf("seventy five seconds into its job the session declared %d jobs, want 1. It said:\n%s",
			len(children), said)
	}
	// From the credential and never from the caller, the same as at the first second of the job.
	child := children[0]
	if child.GetParent() != declared {
		t.Fatalf("the sub job hangs under %q, want the job the session was running, %q", child.GetParent(), declared)
	}
	if child.GetDepth() != 1 {
		t.Fatalf("the sub job is at depth %d, want 1", child.GetDepth())
	}
}

// within waits for something to become true, for as long as the test's own deadline allows.
func within(ctx context.Context, done func() bool) bool {
	for ctx.Err() == nil {
		if done() {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

// TestASessionCannotOpenAConnectionToPostgres.
//
// The store is on the system's own network and a session is not, which is the whole reason the session
// network exists rather than the system's being widened. A session runs model output, so what else is
// on its network is the whole of the boundary.
//
// Both halves are asserted, because either alone can pass for the wrong reason: the name does not
// resolve, and the address it would resolve to accepts nothing. And the same command against the
// system's own address has to succeed, or this test proves only that the command is broken.
func TestASessionCannotOpenAConnectionToPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	system := aSystemWhoseSessionsCanReachIt(ctx, t)

	where := system.postgresOnTheSystemsNetwork(ctx, t)
	declared := system.declare(ctx, t, "clear the backlog", assessorRole)

	said := system.run(ctx, t, declared, connects+strings.Join([]string{
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

// The roles this file imports. One carries job.create and job.read, which is what the assessor role
// this system ships carries; the other carries neither, and exists to be named by a sub job.
const (
	assessorRole    = "assessor-under-test"
	implementerRole = "implementer-under-test"
)

// reachableSystem is a control plane serving on the host, with its sessions on a network of their own.
type reachableSystem struct {
	server         *controlplane.Server
	projectID      string
	workspaceID    string
	sessionNetwork string
	systemNetwork  string
	// lastSession is the session the last task ran in, which is what a sub job's own session has to
	// be different from.
	lastSession string
}

// aSystemWhoseSessionsCanReachIt builds the whole thing: two networks, a control plane serving on the
// host with the real interceptor in front of it, and a workspace holding two roles.
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
		// What the system tells a session running a job, beside the credential it mints for
		// that job. The two together are the whole of what a session is given.
		Reachable: fmt.Sprintf("%s:%d", gateway, port),
	})

	// The guard the composed system runs behind, and the reason this is worth doing over a container:
	// the refusal has to come from the interceptor and the policy rather than from a test.
	grpcServer := grpc.NewServer(auth.ServerOptions(auth.Policy{
		Token:       "the-operator's-token",
		Grants:      server.Grants(),
		DeniedToJob: controlplane.DeniedToJob,
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

	// Depth starts at zero, so no session declares anything until an operator raises it.
	if _, err := server.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: system.workspaceID, MaxDepth: 2},
	}); err != nil {
		t.Fatalf("raise the ceiling: %v", err)
	}
	system.holdRole(ctx, t, assessorRole, role.VerbJobCreate, role.VerbJobRead)
	system.holdRole(ctx, t, implementerRole, role.VerbJobRead)
	return system
}

// holdRole imports a role declaring the verbs named and attaches it to the workspace. A role is what
// grants: job running as none holds a credential that may call nothing.
func (c *reachableSystem) holdRole(ctx context.Context, t *testing.T, name string, verbs ...string) {
	t.Helper()
	manifest := fmt.Sprintf("name: %s\nversion: 1\nsummary: a role for this test\nmodel: opus\nreceives:\n  - job\nverbs:\n",
		name)
	for _, verb := range verbs {
		manifest += "  - " + verb + "\n"
	}
	if _, err := c.server.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: []*quaycrewv1.RoleFile{
			{Path: role.ManifestFile, Body: []byte(manifest)},
			{Path: role.BriefFile, Body: []byte("Clear the backlog.")},
		},
	}); err != nil {
		t.Fatalf("import the role %s: %v", name, err)
	}
	if _, err := c.server.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: c.workspaceID, Name: name,
	}); err != nil {
		t.Fatalf("attach the role %s: %v", name, err)
	}
}

// declare records a job as the operator does, and hands back its identifier.
func (c *reachableSystem) declare(ctx context.Context, t *testing.T, title, named string) string {
	t.Helper()
	declared, err := c.server.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: c.projectID, Title: title, Brief: "read the open pull requests", Role: named,
	})
	if err != nil {
		t.Fatalf("declare the job: %v", err)
	}
	return declared.GetJob().GetId()
}

// run dispatches a task for a job and returns what the session said.
//
// The script runs inside the session's container, in the environment the system built for that one
// task, which is where the address and the credential are: a sandbox keeps what it was born with, so
// a credential written at birth would label every later task with the first task's grant.
func (c *reachableSystem) run(ctx context.Context, t *testing.T, declared, script string) string {
	t.Helper()
	dispatched, err := c.server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: c.projectID, Text: script, Job: declared,
	})
	if err != nil {
		t.Fatalf("dispatch the task: %v", err)
	}
	c.removeSandbox(t, dispatched.GetId())
	c.lastSession = dispatched.GetId()
	return dispatched.GetReply()
}

func (c *reachableSystem) phaseOf(ctx context.Context, t *testing.T, declared string) string {
	t.Helper()
	return c.jobNamed(ctx, t, declared).GetPhase()
}

func (c *reachableSystem) jobNamed(ctx context.Context, t *testing.T, id string) *quaycrewv1.Job {
	t.Helper()
	held, err := c.server.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
	if err != nil {
		t.Fatalf("read the job back: %v", err)
	}
	return held.GetJob()
}

// children is what one job has hanging under it.
func (c *reachableSystem) children(ctx context.Context, t *testing.T, parent string) []*quaycrewv1.Job {
	t.Helper()
	listed, err := c.server.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: c.projectID, Parent: parent})
	if err != nil {
		t.Fatalf("list what hangs under the job: %v", err)
	}
	return listed.GetJobs()
}

// postgresOnTheSystemsNetwork starts a real store where the composed stack keeps it, on the system's own
// network and on no other, and answers with its address there.
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

// shellRunner runs the task's text as a command inside the session's sandbox, with the environment
// the system built for that task.
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
	// `krewe` exits non zero when the system refuses it, so a runner that failed the task would throw
	// away the sentence being asserted on.
	_ = proc.Wait()
	if readErr != nil {
		return model.Response{}, fmt.Errorf("model: read what the session said: %w", readErr)
	}
	return model.Response{Reply: string(said), ModelSessionID: "shell"}, nil
}
