package controlplane

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/contextsize"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// What a project is for, and what was designed for it. Three calls, beside the context calls in
// server.go, because a design is the same kind of thing: text the operator writes about a project,
// kept by the system rather than in a file somebody has to find.
//
// A design is a separate table from a project because a body is the largest text in the system and
// every project listing reads the project row.

// briefMark is the length past which a brief is long enough to say so. A brief is one paragraph
// naming what the project is for, and a page of prose in that field is a design in the wrong column.
//
// bodyMark is the same for the design body. It is far larger because a design is expected to be
// long, and the number is there to catch a whole repository pasted in.
//
// Neither refuses anything. The text is kept whole either way, and the caller is told the length so
// a person decides. Refusing here would lose work that only exists in the call being made.
const (
	briefMark = 2_000
	bodyMark  = 100_000
)

// GetDesign returns what a project is for and what was designed for it.
//
// A project with no design row answers with an empty design rather than an error, because nothing
// written is the normal state. Reading records nothing.
func (s *Server) GetDesign(ctx context.Context, req *quaycrewv1.GetDesignRequest) (*quaycrewv1.GetDesignResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "which project: say where with an address")
	}
	design, err := s.store.GetDesign(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.GetDesignResponse{Design: design}, nil
}

// SetBrief records what a project is for. The brief is one paragraph, and it is kept whole however
// long it is.
func (s *Server) SetBrief(ctx context.Context, req *quaycrewv1.SetBriefRequest) (*quaycrewv1.SetBriefResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "which project: say where with an address")
	}
	design, err := s.store.SetProjectBrief(ctx, req.GetProject(), req.GetBrief())
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.SetBriefResponse{
		Design:   design,
		Warnings: overMark("brief", len(req.GetBrief()), briefMark, "one paragraph saying what the project is for"),
	}, nil
}

// SetDesign records the design document whole, and who wrote it.
//
// written_by is what the caller claimed and the system does not check it. It grants nothing, so a
// wrong one costs a wrong name in a record rather than a capability.
func (s *Server) SetDesign(ctx context.Context, req *quaycrewv1.SetDesignRequest) (*quaycrewv1.SetDesignResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "which project: say where with an address")
	}
	design, err := s.store.SetProjectDesign(ctx, req.GetProject(), req.GetBody(), req.GetWrittenBy())
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.SetDesignResponse{
		Design:   design,
		Warnings: overMark("design", len(req.GetBody()), bodyMark, "the design document"),
	}, nil
}

// overMark says how long the text is when it is past the mark, and says nothing at all when it is
// not. It is a warning and never a refusal: the text is already kept.
func overMark(what string, length, mark int, expected string) []string {
	if length <= mark {
		return nil
	}
	return []string{fmt.Sprintf("the %s is %s, over the %s mark. It is kept whole. A %s is %s.",
		what, contextsize.Characters(length), contextsize.Characters(mark), what, expected)}
}
