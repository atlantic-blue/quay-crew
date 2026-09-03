package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Work a session left behind, reached without anybody opening a container.

// WorkFileCeiling is how much of a file this will hand back. A working directory holds whatever the
// session put there, a build output among it, and a call that would carry a gigabyte through the
// system is a call that takes the system down rather than answering.
const WorkFileCeiling = 1 << 20

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
