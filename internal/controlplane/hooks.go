package controlplane

import (
	"context"
	"errors"
	"sort"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/hook"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The four calls that make a hook reachable from outside. They mirror the skill four, because a hook
// is authored, imported and attached the same way, and they are separate calls because the two
// entities are separate things.

// ImportHook takes a hook into the crew from the files a client read out of its directory.
//
// The files travel and this side validates, for the reason ImportSkill gives: the control plane runs
// in a container where a path on the operator's machine means nothing, and one validator is one
// answer. It matters more here, because a client that checked for itself would be a second set of
// rules deciding whether a constraint is real.
func (s *Server) ImportHook(ctx context.Context, req *quaycrewv1.ImportHookRequest) (*quaycrewv1.ImportHookResponse, error) {
	files := make([]hook.File, 0, len(req.GetFiles()))
	for _, file := range req.GetFiles() {
		files = append(files, hook.File{
			Path:       file.GetPath(),
			Body:       file.GetBody(),
			Executable: file.GetExecutable(),
		})
	}
	loaded, err := hook.FromFiles(files)
	if err != nil {
		// The hook package's own sentence, which names what is wrong and what to do about it.
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := s.store.ImportHook(ctx, store.ImportedHook{Hook: loaded}); err != nil {
		if errors.Is(err, store.ErrHookChanged) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"%s version %d is already imported and is a different hook. Raise the version in %s: a workspace pins the version it holds, so changing one underneath it would change the constraint a running session is under.",
				loaded.Name, loaded.Version, hook.ManifestFile)
		}
		return nil, storeError(err, "import hook")
	}
	stored, err := s.store.GetHook(ctx, loaded.Name, loaded.Version)
	if err != nil {
		return nil, storeError(err, "read the imported hook")
	}
	return &quaycrewv1.ImportHookResponse{Hook: asHook(stored)}, nil
}

// ListHooks says what the crew holds, what one workspace holds, or what one thread runs under.
func (s *Server) ListHooks(ctx context.Context, req *quaycrewv1.ListHooksRequest) (*quaycrewv1.ListHooksResponse, error) {
	// A thread's listing is the same answer its sandbox is built from, so the listing cannot say one
	// thing while the sandbox does another.
	if req.GetThread() != "" {
		session, err := s.store.GetSession(ctx, req.GetThread())
		if err != nil {
			return nil, storeError(err, "thread")
		}
		held := s.hooksFor(ctx, session.GetWorkspace())
		out := make([]*quaycrewv1.Hook, 0, len(held))
		for _, one := range held {
			out = append(out, asHook(one))
		}
		sort.Slice(out, func(i, j int) bool { return out[i].GetName() < out[j].GetName() })
		return &quaycrewv1.ListHooksResponse{Hooks: out}, nil
	}

	var held []store.ImportedHook
	var err error
	if req.GetWorkspace() != "" {
		held = s.hooksFor(ctx, req.GetWorkspace())
	} else {
		held, err = s.store.ListHooks(ctx)
	}
	if err != nil {
		return nil, storeError(err, "list hooks")
	}
	// Which of them the crew holds, so a listing says where a hook came from rather than leaving the
	// operator to guess why a workspace they attached nothing to has three.
	crew := map[string]bool{}
	if fromCrew, err := s.store.CrewHooks(ctx); err == nil {
		for _, one := range fromCrew {
			crew[one.Name] = true
		}
	}
	out := make([]*quaycrewv1.Hook, 0, len(held))
	for _, one := range held {
		carried := asHook(one)
		carried.Crew = crew[one.Name]
		out = append(out, carried)
	}
	return &quaycrewv1.ListHooksResponse{Hooks: out}, nil
}

// AttachHook gives a workspace a hook, for every sandbox born from now on.
//
// A session already running keeps what its sandbox was born with. The files are mounted and the
// settings rendered at container creation, so a hook attached now does not reach a container that
// already exists, and saying otherwise would be worse than saying nothing: the operator would
// believe a gate is on.
func (s *Server) AttachHook(ctx context.Context, req *quaycrewv1.AttachHookRequest) (*quaycrewv1.AttachHookResponse, error) {
	if req.GetScope() == crewScope {
		attached, err := s.store.AttachCrewHook(ctx, req.GetName())
		if err != nil {
			return nil, storeError(err, "attach hook")
		}
		carried := asHook(attached)
		carried.Crew = true
		return &quaycrewv1.AttachHookResponse{Hook: carried}, nil
	}
	attached, err := s.store.AttachHook(ctx, req.GetWorkspace(), req.GetName())
	if err != nil {
		return nil, storeError(err, "attach hook")
	}
	return &quaycrewv1.AttachHookResponse{Hook: asHook(attached)}, nil
}

// DetachHook takes a hook away from a workspace. The hook stays imported.
func (s *Server) DetachHook(ctx context.Context, req *quaycrewv1.DetachHookRequest) (*quaycrewv1.DetachHookResponse, error) {
	if req.GetScope() == crewScope {
		if err := s.store.DetachCrewHook(ctx, req.GetName()); err != nil {
			return nil, storeError(err, "detach hook")
		}
		return &quaycrewv1.DetachHookResponse{}, nil
	}
	if err := s.store.DetachHook(ctx, req.GetWorkspace(), req.GetName()); err != nil {
		return nil, storeError(err, "detach hook")
	}
	return &quaycrewv1.DetachHookResponse{}, nil
}

// hooksFor is every hook a workspace's sessions run under: its own and the crew's, the workspace
// winning a name collision because the narrower statement is the more deliberate one.
//
// A failure reading either is not a failure of the caller. Fewer hooks is a weaker crew and no
// answer at all is a broken one, and the listing is what an operator reads to find out which.
func (s *Server) hooksFor(ctx context.Context, workspace string) []store.ImportedHook {
	held, err := s.store.WorkspaceHooks(ctx, workspace)
	if err != nil {
		held = nil
	}
	crew, err := s.store.CrewHooks(ctx)
	if err == nil {
		for _, one := range crew {
			if containsHook(held, one.Name) {
				continue
			}
			held = append(held, one)
		}
	}
	sort.Slice(held, func(i, j int) bool { return held[i].Name < held[j].Name })
	return held
}

func containsHook(held []store.ImportedHook, name string) bool {
	for _, one := range held {
		if one.Name == name {
			return true
		}
	}
	return false
}

// asHook renders a hook for a client. The files never travel back: a client asked what the crew
// enforces, not for a copy of every script.
func asHook(one store.ImportedHook) *quaycrewv1.Hook {
	out := &quaycrewv1.Hook{
		Name:     one.Name,
		Version:  int32(one.Version),
		Summary:  one.Summary,
		Binaries: one.Binaries,
	}
	for _, binding := range one.Events {
		out.Events = append(out.Events, &quaycrewv1.HookBinding{
			On:             binding.On,
			Matcher:        binding.Matcher,
			Entry:          binding.Entry,
			TimeoutSeconds: int32(binding.TimeoutSeconds),
		})
	}
	for _, name := range one.SecretNames() {
		out.Secrets = append(out.Secrets, &quaycrewv1.SkillSecret{
			Name: name, Purpose: one.Secrets[name],
		})
	}
	if !one.ImportedAt.IsZero() {
		out.ImportedAt = timestamppb.New(one.ImportedAt)
	}
	return out
}
