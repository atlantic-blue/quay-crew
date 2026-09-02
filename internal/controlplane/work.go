package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/publish"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Work a session finished, reached without anybody opening a container.
//
// Both halves of issue 545 are here. The system publishes what a finished job left behind, and it
// answers for what is in a session's directory, so an operator who reads a path can act on it rather
// than becoming the transport.

// WorkFileCeiling is how much of a file this will hand back. A working directory holds whatever the
// session put there, a build output among it, and a call that would carry a gigabyte through the
// system is a call that takes the system down rather than answering.
const WorkFileCeiling = 1 << 20

// PublishSessionWork pushes the branch this session's work is on, and says what it found.
//
// The push needs no approval because a push applies nothing: it makes the branch readable and changes
// nothing about what is deployed. The pull request and the merge are decisions and stay with a
// person, so nothing here opens either.
//
// Git runs inside the session's container. That is where git is, and it is where the credential that
// reaches the remote is: this process is a static binary with no shell, and the token belongs to the
// workspace rather than to the system. A session with no container is read as exactly that, never as
// a session with no work.
func (s *Server) PublishSessionWork(ctx context.Context, sessionID string) publish.Work {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return publish.Work{State: publish.Unreadable, Why: "the system could not read the session the job ran in"}
	}
	places := s.storage.WorkPlaces(boxOf(session))
	if len(places) == 0 {
		return publish.Work{State: publish.Unreadable, Why: "this system keeps no working directory on disk"}
	}
	place, held := sandbox.Repository(places)
	if !held {
		// A session that cloned nothing. Its own directory is still named, because whatever it wrote is
		// in there and the operator has to be told where rather than that there is nothing.
		return publish.Work{State: publish.Absent, Host: places[0].Host}
	}
	box, live, err := s.provider.Existing(ctx, sessionID)
	switch {
	case err != nil:
		return publish.Work{State: publish.Unreadable, Host: place.Host,
			Why: "the system could not tell whether the session still has a container"}
	case !live:
		// Read with no sandbox, which answers unreadable and names the path. Going through the same
		// call rather than building the answer here keeps one description of what that state is.
		return publish.Read(ctx, nil, place)
	}
	// The session's own environment travels with each command, so git reaches the remote with the
	// workspace's credential the way a task would. The container already carries these values; sending
	// them again is what makes this work on a container born before the operator set the token.
	return publish.Read(ctx, carrying{box: box, env: environ(s.taskEnv(ctx, session, "", false))}, place)
}

// PushSessionWork puts what this session committed onto one named branch, and says what it found.
//
// It is the same road as the call above and answers in the same words. What differs is whose branch
// the work goes on: there the session chose it, and here the branch belongs to the job, because
// several sessions at once are writing work that has to arrive in one place for the next stage to
// read. A push that finds somebody already there is replayed onto them rather than refused.
func (s *Server) PushSessionWork(ctx context.Context, sessionID, branch string) publish.Work {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return publish.Work{State: publish.Unreadable, Why: "the system could not read the session the job ran in"}
	}
	places := s.storage.WorkPlaces(boxOf(session))
	if len(places) == 0 {
		return publish.Work{State: publish.Unreadable, Why: "this system keeps no working directory on disk"}
	}
	place, held := sandbox.Repository(places)
	if !held {
		// A session that cloned nothing. Its own directory is still named, because whatever it wrote is
		// in there and the operator has to be told where rather than that there is nothing.
		return publish.Work{State: publish.Absent, Host: places[0].Host}
	}
	box, live, err := s.provider.Existing(ctx, sessionID)
	switch {
	case err != nil:
		return publish.Work{State: publish.Unreadable, Host: place.Host,
			Why: "the system could not tell whether the session still has a container"}
	case !live:
		return publish.Deliver(ctx, nil, place, branch)
	}
	return publish.Deliver(ctx, carrying{box: box, env: environ(s.taskEnv(ctx, session, "", false))},
		place, branch)
}

// carrying is a sandbox whose every command is given the session's environment.
type carrying struct {
	box sandbox.Sandbox
	env []string
}

func (c carrying) Exec(ctx context.Context, spec sandbox.Spec) (sandbox.Process, error) {
	// The session's first and the command's second, so anything the caller set wins, which is the
	// order a task already gets.
	spec.Env = append(append([]string(nil), c.env...), spec.Env...)
	return c.box.Exec(ctx, spec)
}

// ReadSessionWork hands back a file, or a listing, out of the work a session left behind.
//
// It reads the directory rather than the container, which is what makes it answer for a session
// whose sandbox has gone. The working directory is state this system keeps on the host and mounts
// in, so these are the same bytes the model was looking at, read without starting anything and
// without a road into a container.
//
// The root is the repository the session worked in where there is one, and the session's own
// directory where there is not. Both are named in the answer, so a caller always knows which
// directory it is looking at.
func (s *Server) ReadSessionWork(ctx context.Context, req *quaycrewv1.ReadSessionWorkRequest) (*quaycrewv1.ReadSessionWorkResponse, error) {
	if req.GetSession() == "" {
		return nil, status.Error(codes.InvalidArgument, "say which session: krewe work <session> [<path>]")
	}
	session, err := s.store.GetSession(ctx, req.GetSession())
	if err != nil {
		return nil, storeError(err, "session")
	}
	places := s.storage.WorkPlaces(boxOf(session))
	if len(places) == 0 {
		return nil, status.Error(codes.FailedPrecondition,
			"this system keeps no working directory on disk, so there is nothing to read")
	}
	root := places[0]
	if found, held := sandbox.Repository(places); held {
		root = found
	}
	// Cleaned onto the root and held inside it. The leading slash is what makes a path that climbs
	// harmless: it is resolved against the root of nothing, so `../../etc` becomes `/etc` and then a
	// name inside the work rather than a road out of it.
	asked := filepath.Clean("/" + req.GetPath())
	inside := filepath.Join(root.Dir, asked)
	relative := strings.TrimPrefix(asked, "/")

	info, err := os.Stat(inside)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound,
				"the session's work at %s holds nothing called %q", root.Host, relative)
		}
		return nil, status.Errorf(codes.Internal, "read %s: %v", relative, err)
	}
	answer := &quaycrewv1.ReadSessionWorkResponse{
		Host: filepath.ToSlash(filepath.Join(root.Host, relative)), Path: relative,
	}
	if info.IsDir() {
		answer.Directory = true
		entries, err := os.ReadDir(inside)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "read %s: %v", relative, err)
		}
		answer.Entries = entriesOf(entries)
		return answer, nil
	}
	if info.Size() > WorkFileCeiling {
		return nil, status.Errorf(codes.FailedPrecondition,
			"%s is %d bytes and this hands back at most %d: read it at %s on the machine running the sandboxes",
			relative, info.Size(), WorkFileCeiling, answer.GetHost())
	}
	body, err := os.ReadFile(inside)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read %s: %v", relative, err)
	}
	answer.Content = body
	return answer, nil
}

// entriesOf is what a directory holds, sorted by name so two reads of one directory answer the same.
func entriesOf(entries []os.DirEntry) []*quaycrewv1.SessionWorkEntry {
	out := make([]*quaycrewv1.SessionWorkEntry, 0, len(entries))
	for _, entry := range entries {
		one := &quaycrewv1.SessionWorkEntry{Name: entry.Name(), Directory: entry.IsDir()}
		if info, err := entry.Info(); err == nil && !entry.IsDir() {
			one.Size = info.Size()
		}
		out = append(out, one)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GetName() < out[j].GetName() })
	return out
}
