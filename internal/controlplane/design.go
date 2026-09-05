package controlplane

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/contextsize"
	"github.com/atlantic-blue/quay-krewe/internal/store"
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
	s.renderDesignTo(ctx, req.GetProject())
	return &quaycrewv1.SetBriefResponse{
		Design:   design,
		Warnings: overMark("brief", utf8.RuneCountInString(req.GetBrief()), briefMark, "one paragraph saying what the project is for"),
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
	s.renderDesignTo(ctx, req.GetProject())
	return &quaycrewv1.SetDesignResponse{
		Design:   design,
		Warnings: overMark("design", utf8.RuneCountInString(req.GetBody()), bodyMark, "the design document"),
	}, nil
}

// ApproveDesign records the operator's word on the design as it stands.
//
// It is the operator's call and nobody else's: DeniedToDriver refuses it to a session, so a session
// that writes a design cannot approve the design it wrote. That refusal is the boundary the whole
// approval rests on.
//
// The call carries no text. The approval is about the body the store holds when it lands, and any
// later write to that body clears it, so there is no way to approve one text and have the word stand
// on another.
func (s *Server) ApproveDesign(ctx context.Context, req *quaycrewv1.ApproveDesignRequest) (*quaycrewv1.ApproveDesignResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "which project: say where with an address")
	}
	design, err := s.store.ApproveProjectDesign(ctx, req.GetProject())
	if errors.Is(err, store.ErrNothingToApprove) {
		return nil, status.Error(codes.FailedPrecondition,
			"there is no design to approve: write one with krewe design set, then approve it")
	}
	if err != nil {
		return nil, storeError(err, "project")
	}
	// Every live session in the project reads the approval in its memory file, so the word has to
	// reach the sessions that are already running rather than only the next sandbox built.
	s.renderDesignTo(ctx, req.GetProject())
	return &quaycrewv1.ApproveDesignResponse{Design: design}, nil
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

// What the session working in a project is told about that project's design, and where the design
// itself is put so the model can open it.
//
// The summary is a section in the inner memory file, which every exec of every session in the
// project reads. That is what the cap below is for: the whole section is read again on every exec,
// so its cost is paid per exec rather than once.
const (
	// designSectionCap is the whole section, in characters. The design body is not in it: the
	// section names a file, and the file is opened by a model that decides it needs it.
	designSectionCap = 400
	// designBriefCap is how much of the brief reaches the section. The store keeps it whole.
	designBriefCap = 200
	// designDir and designFile are where the design body goes in the session's working directory. A
	// dot directory, because a repository cloned into the working directory may hold a file called
	// design.md of its own.
	designDir  = ".krewe"
	designFile = "design.md"
)

// renderDesign puts the design body in the session's working directory and returns the summary that
// goes in its memory file. It returns the empty string when there is nothing to say, and Compose
// drops an empty section.
//
// The file and the summary are written together, for the reason renderSkills gives: a line telling
// the model to read a file that is not there sends it to open nothing.
//
// Nothing here fails an exec. A session with no design summary is a session that reads the project's
// context and gets on with it, which is what every session did before this existed.
func (s *Server) renderDesign(ctx context.Context, session *quaycrewv1.Session, dir string) string {
	design, err := s.store.GetDesign(ctx, session.GetProject())
	if err != nil {
		return ""
	}
	if design.GetBrief() == "" && design.GetBody() == "" {
		s.clearDesignFile(dir)
		return ""
	}
	project, err := s.store.GetProject(ctx, session.GetProject())
	if err != nil {
		return ""
	}
	hasBody := s.writeDesignFile(dir, design.GetBody())
	return designSummary(project.GetName(), design, hasBody)
}

// writeDesignFile puts the design body where the model can open it, and says whether it is there to
// open. An empty body has no file: a file that exists and says nothing costs a read.
func (s *Server) writeDesignFile(dir, body string) bool {
	if body == "" {
		s.clearDesignFile(dir)
		return false
	}
	at := filepath.Join(dir, designDir)
	if err := os.MkdirAll(at, 0o755); err != nil {
		slog.Warn("the design was not written where the session can read it", "at", at, "error", err)
		return false
	}
	if err := os.WriteFile(filepath.Join(at, designFile), []byte(body), 0o644); err != nil {
		slog.Warn("the design was not written where the session can read it", "at", at, "error", err)
		return false
	}
	return true
}

// clearDesignFile takes the file away when the store holds no body, so what the session reads and
// what the store holds cannot disagree. A design emptied on purpose would otherwise stay readable
// for the life of the working directory.
func (s *Server) clearDesignFile(dir string) {
	if err := os.Remove(filepath.Join(dir, designDir, designFile)); err != nil && !os.IsNotExist(err) {
		slog.Warn("the old design was left where the session can read it", "dir", dir, "error", err)
	}
}

// designSummary is the section itself.
//
// The approval line is the point of the section rather than a detail of it. A design nobody approved
// is a design nothing is built from, so the session is told that in the file it reads on every exec,
// beside the line that sends it to the document. There is no approval line when there is no design
// body, because there is nothing to approve.
//
// The brief is cut to fit rather than the section being allowed to grow, because the section is read
// on every exec and the brief is the only part of it whose length nobody controls. A cut line ends
// with a full stop and nothing else: an ellipsis or a note saying the text was cut would tell the
// model to go looking for the rest, and the rest is in the design.
func designSummary(project string, design *quaycrewv1.Design, hasBody bool) string {
	brief := design.GetBrief()
	lines := []string{"This project is " + project + "."}
	if hasBody {
		lines = append(lines, approvalLine(design), "Read "+designDir+"/"+designFile+" before you start.")
	}
	if brief == "" {
		return strings.Join(lines, "\n")
	}

	// What is left for the brief once the rest of the section is written. The two is the " It is
	// for: " separator's own cost against the line it joins, counted here rather than guessed.
	spent := utf8.RuneCountInString(strings.Join(lines, "\n")) + utf8.RuneCountInString(" It is for: ")
	room := min(designBriefCap, designSectionCap-spent)
	if room <= 0 {
		return strings.Join(lines, "\n")
	}
	lines[0] += " It is for: " + cutTo(brief, room)
	return strings.Join(lines, "\n")
}

// approvalLine is what the session is told about the operator's word on this design.
//
// The date and nothing finer. A session decides whether to build from the design, and the minute the
// operator typed the command does not change that answer.
func approvalLine(design *quaycrewv1.Design) string {
	if !design.GetApproved() {
		return "The design is not approved yet. Build nothing from it until the operator approves it."
	}
	return "The design was approved on " + design.GetApprovedAt().AsTime().Format("2006-01-02") + "."
}

// cutTo shortens text to a number of characters, at a word boundary where there is one, and ends it
// with a full stop. Text already short enough is the operator's own and is left exactly as it is.
func cutTo(text string, room int) string {
	if utf8.RuneCountInString(text) <= room {
		return text
	}
	// One character of the room is the full stop this ends with.
	runes := []rune(text)[:room-1]
	cut := strings.TrimRight(string(runes), " \t\n.,;:")
	if at := strings.LastIndexAny(cut, " \t\n"); at > 0 {
		cut = strings.TrimRight(cut[:at], " \t\n.,;:")
	}
	return cut + "."
}

// renderDesignTo puts a changed design in front of every live session in the project.
//
// Without it a design only reached a session when its sandbox was built, so writing one while a
// session was running did nothing that session could see, and nobody replaces a container to deliver
// a document. It is the same reason SetContext renders, and the same call underneath.
func (s *Server) renderDesignTo(ctx context.Context, project string) {
	s.renderTo(ctx, store.ContextProject, project)
}
