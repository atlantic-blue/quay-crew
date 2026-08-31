package controlplane_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/publish"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/store"
)

// Work a job finished, driven through the control plane against real git and a real remote.
//
// The remote is a bare repository on disk rather than a forge, so what is proved here is the whole
// road: a job that names a repository and answers twice without a pull request, a controller that
// stops it, and a branch that is on the remote afterwards. An assertion that the system called push
// would have checked the easy half.

// mountedProvider is a container whose bind mounts are real: a command it runs lands in the directory
// the system mounted at that path, which is exactly what a container does with a session's working
// directory and its workspace's volume.
//
// It runs git and nothing else. Git is what this proves, and the other commands the system runs at
// sandbox birth configure the machine they run on, which on a host is the operator's own.
type mountedProvider struct {
	storage sandbox.Storage
	mu      sync.Mutex
	boxes   map[string]*mountedSandbox
}

func (p *mountedProvider) Create(_ context.Context, cfg sandbox.Config) (sandbox.Sandbox, error) {
	// The directories first, the way the Docker provider makes them before it starts a container.
	if _, err := p.storage.Prepare(cfg); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.boxes == nil {
		p.boxes = map[string]*mountedSandbox{}
	}
	if held, live := p.boxes[cfg.ID]; live {
		return held, nil
	}
	box := &mountedSandbox{mounts: p.storage.WorkPlaces(cfg)}
	p.boxes[cfg.ID] = box
	return box, nil
}

func (p *mountedProvider) Existing(_ context.Context, sessionID string) (sandbox.Sandbox, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	box, live := p.boxes[sessionID]
	return box, live, nil
}

func (p *mountedProvider) Remove(_ context.Context, sessionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.boxes, sessionID)
	return nil
}

func (p *mountedProvider) Stranded(context.Context) ([]string, error)           { return nil, nil }
func (p *mountedProvider) Attached(context.Context, string) (bool, error)       { return false, nil }
func (p *mountedProvider) RuntimeRunning(context.Context, string) (bool, error) { return false, nil }

type mountedSandbox struct{ mounts []sandbox.Place }

func (m *mountedSandbox) Exec(ctx context.Context, spec sandbox.Spec) (sandbox.Process, error) {
	if len(spec.Argv) == 0 || spec.Argv[0] != "git" {
		return nothingRan{}, nil
	}
	dir, mounted := m.onTheHost(spec.Workdir)
	if !mounted {
		return nil, errors.New("no such directory in this container: " + spec.Workdir)
	}
	command := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	command.Dir = dir
	// A closed environment, so git reads this test's configuration and never the operator's.
	command.Env = append([]string{"HOME=" + dir, "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null", "PATH=" + os.Getenv("PATH")}, spec.Env...)
	out, err := command.Output()
	said := ""
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		said = string(exited.Stderr)
	}
	return ranned{out: string(out), stderr: said, err: err}, nil
}

func (m *mountedSandbox) Close(context.Context) error { return nil }

// onTheHost is where a path inside the container actually is, which is what a bind mount answers.
func (m *mountedSandbox) onTheHost(inside string) (string, bool) {
	for _, mount := range m.mounts {
		if inside == mount.Sandbox {
			return mount.Dir, true
		}
		if under, held := strings.CutPrefix(inside, mount.Sandbox+"/"); held {
			return filepath.Join(mount.Dir, under), true
		}
	}
	return "", false
}

type nothingRan struct{}

func (nothingRan) Stdout() io.Reader { return strings.NewReader("") }
func (nothingRan) Wait() error       { return nil }
func (nothingRan) Stderr() string    { return "" }

type ranned struct {
	out    string
	stderr string
	err    error
}

func (r ranned) Stdout() io.Reader { return strings.NewReader(r.out) }
func (r ranned) Wait() error       { return r.err }
func (r ranned) Stderr() string    { return r.stderr }

// aJobThatWillNotName is a job in a repository whose session answers twice without a pull request,
// stopped at the point where the system goes to look at what it left behind.
type aJobThatWillNotName struct {
	server  *controlplane.Server
	storage sandbox.Storage
	job     *quaycrewv1.Job
	session string
	// work is the session's own working directory on this machine, which is where the test builds
	// whatever the session is supposed to have made.
	work string
	// remote is the bare repository standing in for the forge.
	remote string
}

func aJobInARepository(t *testing.T) *aJobThatWillNotName {
	t.Helper()
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: dir}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "I made the change and the tests pass"},
		Provider: &mountedProvider{storage: storage}, Secrets: secrets.NewMemory(), Storage: storage,
	})
	_, project := newProject(t, server)
	declared, err := server.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "sort the listing", Brief: "make the listing sort by the clock it shows",
		Repository: "atlantic-blue/krewe",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	system := &aJobThatWillNotName{
		server: server, storage: storage, job: declared.GetJob(),
		remote: filepath.Join(dir, "remote.git"),
	}
	// The first task, so the session and its directory exist before the test writes anything into it.
	server.TickJob(context.Background())
	waitFor(t, func() bool {
		sent := tasksOf(t, server, system.job.GetId())
		return len(sent) == 1 && sent[0].GetStatus() != job.StatusRunning
	})
	system.session = system.reading(t).GetSession()
	dirFor, kept := storage.WorkingDir(sandbox.Config{
		ID: system.session, Workspace: system.job.GetWorkspace(), Project: system.job.GetProject(),
	})
	if !kept {
		t.Fatal("the system keeps no working directory for the session it just made")
	}
	system.work = dirFor
	return system
}

func (s *aJobThatWillNotName) reading(t *testing.T) *quaycrewv1.Job {
	t.Helper()
	found, err := s.server.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: s.job.GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return found.GetJob()
}

// stops drives the job to its end: the answer that names no pull request, the ask, the answer that
// still names none, and the tick that stops it.
func (s *aJobThatWillNotName) stops(t *testing.T) *quaycrewv1.Job {
	t.Helper()
	ctx := context.Background()
	s.server.TickJob(ctx)
	waitFor(t, func() bool { return len(tasksOf(t, s.server, s.job.GetId())) == 2 })
	waitFor(t, func() bool {
		sent := tasksOf(t, s.server, s.job.GetId())
		return sent[1].GetStatus() != job.StatusRunning
	})
	s.server.TickJob(ctx)
	s.server.TickJob(ctx)
	return s.reading(t)
}

// The empty case, first. A session that cloned nothing must not be reported as holding a branch, and
// the reason has to name the directory so the operator can look for themselves.
func TestAJobWhoseSessionClonedNothingStopsSayingWhereTheDirectoryIs(t *testing.T) {
	system := aJobInARepository(t)

	stopped := system.stops(t)

	if stopped.GetPhase() != job.PhaseStopped {
		t.Fatalf("the job is %q saying %q, want stopped", stopped.GetPhase(), stopped.GetReason())
	}
	if !strings.Contains(stopped.GetReason(), "no repository") {
		t.Fatalf("the reason is %q, want it to say the session holds no repository", stopped.GetReason())
	}
	if !strings.Contains(stopped.GetReason(), system.work) {
		t.Fatalf("the reason is %q, want it to name the directory %s", stopped.GetReason(), system.work)
	}
}

// A session that cloned and never committed. The branch it cut is not work, so the reason must not
// name it: an operator sent to a branch nobody made goes looking for something that is not there.
func TestAJobWhoseSessionCommittedNothingNamesNoBranchAndPushesNothing(t *testing.T) {
	system := aJobInARepository(t)
	system.aRepository(t)
	system.git(t, "switch", "-c", "sort-the-listing")

	stopped := system.stops(t)

	if !strings.Contains(stopped.GetReason(), "committed nothing") {
		t.Fatalf("the reason is %q, want it to say the session committed nothing", stopped.GetReason())
	}
	if strings.Contains(stopped.GetReason(), "sort-the-listing") {
		t.Fatalf("the reason names a branch with nothing on it:\n%s", stopped.GetReason())
	}
	if system.onTheRemote(t, "sort-the-listing") {
		t.Fatalf("the system pushed a branch with nothing on it")
	}
}

// The whole road. Work that was committed and never pushed is on the remote after the job stops,
// which is the difference between a branch somebody else can read and a directory nobody can reach.
func TestTheBranchOfAJobThatStoppedWithoutAPullRequestReachesTheRemote(t *testing.T) {
	system := aJobInARepository(t)
	system.aRepository(t)
	system.git(t, "switch", "-c", "sort-the-listing")
	system.commits(t, "listing.go", "sorted by the clock it shows")

	stopped := system.stops(t)

	if !system.onTheRemote(t, "sort-the-listing") {
		t.Fatalf("the branch is not on the remote, so the work reached nobody. The job says: %s",
			stopped.GetReason())
	}
	if !strings.Contains(stopped.GetReason(), "pushed the branch sort-the-listing") {
		t.Fatalf("the reason is %q, want it to say the system pushed the branch", stopped.GetReason())
	}
	// Pushed and nothing else. A pull request is a decision and a merge spends money, so the system
	// leaves both, and the reason says which step is left.
	if !strings.Contains(stopped.GetReason(), "open the pull request") {
		t.Fatalf("the reason is %q, want it to say what is left to do", stopped.GetReason())
	}
	// The remote holds what the session made, rather than an empty branch with the right name.
	if held := system.onTheRemoteAt(t, "sort-the-listing", "listing.go"); held != "sorted by the clock it shows\n" {
		t.Fatalf("the remote holds %q on that branch, want what the session wrote", held)
	}
}

// A remote that refuses. The branch and the path are what the operator acts on, and what git said is
// how they work out which of a dozen refusals it was.
func TestWorkAPushCouldNotDeliverIsNamedWithItsBranchAndItsPath(t *testing.T) {
	system := aJobInARepository(t)
	system.aRepository(t)
	system.git(t, "switch", "-c", "sort-the-listing")
	system.commits(t, "listing.go", "sorted by the clock it shows")
	// The remote goes away under it, which is every reason a push fails from the system's side: the
	// address is wrong, the credential is missing, the branch is protected.
	if err := os.RemoveAll(system.remote); err != nil {
		t.Fatal(err)
	}

	stopped := system.stops(t)

	for _, want := range []string{"could not push the branch sort-the-listing", system.work} {
		if !strings.Contains(stopped.GetReason(), want) {
			t.Fatalf("the reason is %q, want it to say %q", stopped.GetReason(), want)
		}
	}
	if !strings.Contains(strings.ToLower(stopped.GetReason()), "does not appear to be a git repository") {
		t.Fatalf("the reason is %q, want it to carry what git said", stopped.GetReason())
	}
}

// And the operator reads the work out of the stopped session, without attaching to it.
func TestTheWorkOfAStoppedJobIsReadWithoutAttachingToTheSession(t *testing.T) {
	system := aJobInARepository(t)
	system.aRepository(t)
	system.git(t, "switch", "-c", "sort-the-listing")
	system.commits(t, "listing.go", "sorted by the clock it shows")
	system.stops(t)
	ctx := context.Background()

	listed, err := system.server.ReadSessionWork(ctx, &quaycrewv1.ReadSessionWorkRequest{Session: system.session})
	if err != nil {
		t.Fatalf("ReadSessionWork: %v", err)
	}
	if !listed.GetDirectory() {
		t.Fatalf("the work reads as a file, want the directory the session worked in")
	}
	if !strings.HasSuffix(listed.GetHost(), "/workspace/krewe") {
		t.Fatalf("the work is at %q, want the repository the session cloned", listed.GetHost())
	}
	if !holdsEntry(listed.GetEntries(), "listing.go") {
		t.Fatalf("the listing does not hold the file the session wrote: %v", listed.GetEntries())
	}

	read, err := system.server.ReadSessionWork(ctx, &quaycrewv1.ReadSessionWorkRequest{
		Session: system.session, Path: "listing.go",
	})
	if err != nil {
		t.Fatalf("ReadSessionWork of a file: %v", err)
	}
	if string(read.GetContent()) != "sorted by the clock it shows\n" {
		t.Fatalf("the file reads %q, want what the session wrote", read.GetContent())
	}
}

func holdsEntry(entries []*quaycrewv1.SessionWorkEntry, want string) bool {
	for _, entry := range entries {
		if entry.GetName() == want {
			return true
		}
	}
	return false
}

// aRepository is a clone in the session's own working directory with a remote behind it, which is
// what the git skill leaves a session holding.
func (s *aJobThatWillNotName) aRepository(t *testing.T) {
	t.Helper()
	bare := exec.Command("git", "init", "--bare", "--initial-branch=main", s.remote)
	if out, err := bare.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, out)
	}
	at := filepath.Join(s.work, "krewe")
	if err := os.MkdirAll(at, 0o777); err != nil {
		t.Fatal(err)
	}
	s.git(t, "init", "--initial-branch=main")
	s.git(t, "config", "user.name", "the operator")
	s.git(t, "config", "user.email", "operator@example.com")
	s.git(t, "config", "commit.gpgsign", "false")
	s.commits(t, "README.md", "the listing")
	s.git(t, "remote", "add", "origin", s.remote)
	s.git(t, "push", "origin", "main")
	s.git(t, "fetch", "origin")
}

// git runs one command in the session's repository, as the session would.
func (s *aJobThatWillNotName) git(t *testing.T, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = filepath.Join(s.work, "krewe")
	command.Env = closedGitEnvironment(command.Dir)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// commits writes a file and commits it, which is what makes a branch worth publishing.
func (s *aJobThatWillNotName) commits(t *testing.T, name, body string) {
	t.Helper()
	at := filepath.Join(s.work, "krewe", name)
	if err := os.WriteFile(at, []byte(body+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	s.git(t, "add", name)
	s.git(t, "commit", "-m", "write "+name)
}

// onTheRemote says whether the bare repository standing in for the forge holds this branch.
func (s *aJobThatWillNotName) onTheRemote(t *testing.T, branch string) bool {
	t.Helper()
	command := exec.Command("git", "--git-dir", s.remote, "rev-parse", "--verify", "--quiet",
		"refs/heads/"+branch)
	command.Env = closedGitEnvironment(s.remote)
	return command.Run() == nil
}

// onTheRemoteAt is what that branch holds in one file on the remote, so a test can say the work
// arrived rather than that a branch of the right name did.
func (s *aJobThatWillNotName) onTheRemoteAt(t *testing.T, branch, name string) string {
	t.Helper()
	command := exec.Command("git", "--git-dir", s.remote, "show", branch+":"+name)
	command.Env = closedGitEnvironment(s.remote)
	out, err := command.Output()
	if err != nil {
		t.Fatalf("git show %s:%s on the remote: %v", branch, name, err)
	}
	return string(out)
}

// closedGitEnvironment keeps git out of the operator's own configuration, so a test can never read
// or write it.
func closedGitEnvironment(home string) []string {
	return []string{"HOME=" + home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"PATH=" + os.Getenv("PATH")}
}

// A daemon that will not say whether the session still has a container. That is the system being
// unable to tell, and it must never be reported as a session with no work: an operator who reads
// "the session committed nothing" stops looking, and the work is still there.
func TestADaemonThatCannotSayWhetherASessionHasAContainerIsReadAsUnreadable(t *testing.T) {
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: dir}
	provider := &sandbox.FakeProvider{
		ExistingErr: errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock"),
	}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "made the change"},
		Provider: provider, Secrets: secrets.NewMemory(), Storage: storage,
	})
	ctx := context.Background()
	_, project := newProject(t, server)
	dispatched, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Text: "make the listing sort by the clock it shows",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	// A repository on disk, so the answer is about the container rather than about there being
	// nothing to find.
	session := dispatched.GetId()
	work, kept := storage.WorkingDir(sandbox.Config{
		ID: session, Workspace: system(t, server, session).GetWorkspace(),
		Project: system(t, server, session).GetProject(),
	})
	if !kept {
		t.Fatal("the system keeps no working directory for the session it just made")
	}
	if err := os.MkdirAll(filepath.Join(work, "krewe", ".git"), 0o777); err != nil {
		t.Fatal(err)
	}

	found := server.PublishSessionWork(ctx, session)

	if found.State != publish.Unreadable {
		t.Fatalf("a daemon that would not answer read as %q, want %q", found.State, publish.Unreadable)
	}
	if found.Host == "" {
		t.Fatal("the system could not say where the work is, and it is the one thing it always knows")
	}
	// An admission, not a claim. "The session has no container" is a thing the system would be saying
	// it knows, and here it does not: the daemon would not answer.
	if !strings.Contains(found.Why, "could not tell") {
		t.Fatalf("the system says %q, want it to say it could not tell", found.Why)
	}
}

// system is the session row, for a test that needs the workspace and project the system put it in.
func system(t *testing.T, server *controlplane.Server, id string) *quaycrewv1.Session {
	t.Helper()
	found, err := server.GetSession(context.Background(), &quaycrewv1.GetSessionRequest{Id: id})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	return found.GetSession()
}
