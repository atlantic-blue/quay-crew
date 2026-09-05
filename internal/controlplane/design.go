package controlplane

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/contextsize"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// What a project is for, and what was designed for it. Four calls, beside the context calls in
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
// It approves the text that is in the store now, and it asks nothing: a call that opened an editor
// or a question would be approving a text nobody named. A design with no body is refused, because
// there is nothing to agree to.
//
// DeniedToDriver refuses this call to a session, so the word reaches the store only through the
// operator's own command. That is the boundary that makes the gate real: a session that could
// approve its own design would be agreeing with itself.
func (s *Server) ApproveDesign(ctx context.Context, req *quaycrewv1.ApproveDesignRequest) (*quaycrewv1.ApproveDesignResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "which project: say where with an address")
	}
	design, err := s.store.ApproveProjectDesign(ctx, req.GetProject())
	if errors.Is(err, store.ErrNothingToApprove) {
		return nil, status.Error(codes.FailedPrecondition,
			"this project has no design to approve: write one with krewe design set [<address>] --file <path>")
	}
	if err != nil {
		return nil, storeError(err, "project")
	}
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
// The approval line is there whenever there is a design to approve, because a session that reads a
// design has to know whether anybody agreed to it. A project holding a brief and no design has
// nothing to say about approval, so it says nothing rather than spending a line of a capped section
// on a word about a document that does not exist.
//
// The brief is cut to fit rather than the section being allowed to grow, because the section is read
// on every exec and the brief is the only part of it whose length nobody controls. A cut line ends
// with a full stop and nothing else: an ellipsis or a note saying the text was cut would tell the
// model to go looking for the rest, and the rest is in the design.
func designSummary(project string, design *quaycrewv1.Design, hasBody bool) string {
	lines := []string{"This project is " + project + "."}
	if hasBody {
		lines = append(lines, approvalLine(design), "Read "+designDir+"/"+designFile+" before you start.")
	}
	brief := design.GetBrief()
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

// approvalLine says where the design stands with the operator, in the one line the session reads on
// every exec. The date and nothing finer: the session decides what to do with the design, and the
// hour it was approved changes none of that.
func approvalLine(design *quaycrewv1.Design) string {
	if !design.GetApproved() {
		return "The design is not approved yet."
	}
	return "The design is approved, on " + design.GetApprovedAt().AsTime().Format("2006-01-02") + "."
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

// The path a design was broken into: the document grammar, and the two calls that write and read it.
//
// The control plane parses the document rather than the caller. That way the command line and the
// console send the same words and cannot drift on the grammar, which is the same reason the control
// plane composes the text a session is dispatched with.

// The five labels a step block carries. Each one sits alone on its line, and a block runs from its
// label to the next label, to the next step heading, or to the end of the document.
const (
	labelIntention = "What changes and why"
	labelTouches   = "What this touches"
	labelProof     = "What proves it"
	labelScenario  = "The scenario that proves it"
	labelAfter     = "After"
)

// stepHeading matches a step's own line: `## <number>. <title>`.
//
// The sign is matched so a number below one is read as a heading with a bad number rather than as
// prose. Left out, `## -1. the store` would be ignored the way a paragraph is, and the refusal about
// a number below one could only ever fire for zero.
var stepHeading = regexp.MustCompile(`^##\s+(-?\d+)\s*\.\s*(.*)$`)

// pathStepMark is the count past which a path is long enough to say so. It refuses nothing: the
// path is kept whole and a person decides.
const pathStepMark = 200

// declaredStep is one step as the document declares it, with the line numbers a refusal has to name.
type declaredStep struct {
	step        store.Step
	headingLine int
	// numberText is the number as the document wrote it, so a refusal quotes what a person typed
	// rather than what it became. A number too large for the column never reaches step.Number.
	numberText string
	// afterSaid is whether the document carried an After block at all, which is a different thing
	// from one that holds nothing. A block holding nothing means zero, and no block at all means the
	// step waits for the number below it.
	afterSaid bool
	afterText string
	afterLine int
	// scenarioExtraLine is the second line of a scenario block, which holds one line and no more.
	scenarioExtraLine int
}

// parsePath reads a path document and returns the steps in ascending number order, with any
// warnings. Every refusal is an InvalidArgument naming the line, so a person can go and fix it.
func parsePath(document string) ([]store.Step, []string, error) {
	declared := readPathDocument(document)
	if len(declared) == 0 {
		return nil, nil, status.Error(codes.InvalidArgument,
			"this document has no steps in it. A step starts with a line reading ## 1. <title>")
	}
	if err := refuseBadHeadings(declared); err != nil {
		return nil, nil, err
	}
	sort.SliceStable(declared, func(i, j int) bool {
		return declared[i].step.Number < declared[j].step.Number
	})
	if err := resolveAfter(declared); err != nil {
		return nil, nil, err
	}

	steps := make([]store.Step, 0, len(declared))
	for _, one := range declared {
		steps = append(steps, one.step)
	}
	return steps, pathWarnings(steps), nil
}

// readPathDocument walks the document once and collects what each step declared.
//
// Text before the first heading is ignored, so a document may carry a title and a paragraph saying
// what the path is for. Every label is optional: a step needs only its heading.
func readPathDocument(document string) []*declaredStep {
	var (
		declared []*declaredStep
		current  *declaredStep
		blocks   map[string][]string
		label    string
	)
	// finish writes the blocks read so far onto the step they belong to. It runs at the next heading
	// and again at the end, because the last step's blocks have no heading after them.
	finish := func() {
		if current == nil {
			return
		}
		current.step.Intention = joinBlock(blocks[labelIntention])
		current.step.Touches = joinBlock(blocks[labelTouches])
		current.step.Proof = joinBlock(blocks[labelProof])
		current.step.ProofScenario = joinBlock(blocks[labelScenario])
		current.afterText = joinBlock(blocks[labelAfter])
		declared = append(declared, current)
	}

	for at, line := range strings.Split(document, "\n") {
		number := at + 1
		if found := stepHeading.FindStringSubmatch(line); found != nil {
			finish()
			// Parsed at the width the column holds, so a number larger than that is refused by the
			// rule below rather than wrapping into a different step silently.
			read, err := strconv.ParseInt(found[1], 10, 32)
			if err != nil {
				read = 0
			}
			current = &declaredStep{
				step:        store.Step{Number: int32(read), Title: strings.TrimSpace(found[2])},
				headingLine: number,
				numberText:  found[1],
			}
			blocks, label = map[string][]string{}, ""
			continue
		}
		if current == nil {
			continue
		}
		if named, is := labelOn(line); is {
			label = named
			if named == labelAfter {
				current.afterSaid, current.afterLine = true, number
			}
			continue
		}
		if label == "" {
			continue
		}
		// The scenario block holds one line, so the second line with anything on it is the line a
		// person has to delete. Blank lines do not count: a block written with a line break under its
		// label still names one scenario. Recorded rather than refused here, so every refusal runs in
		// one place.
		if label == labelScenario && strings.TrimSpace(line) != "" && current.scenarioExtraLine == 0 &&
			saidSomething(blocks[labelScenario]) {
			current.scenarioExtraLine = number
		}
		blocks[label] = append(blocks[label], line)
	}
	finish()
	return declared
}

// saidSomething says whether a block already holds a line with anything on it, so a blank line
// under a label is not read as a second answer.
func saidSomething(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

// labelOn says whether a line is one of the five labels, alone on its line.
func labelOn(line string) (string, bool) {
	switch strings.TrimSpace(line) {
	case labelIntention:
		return labelIntention, true
	case labelTouches:
		return labelTouches, true
	case labelProof:
		return labelProof, true
	case labelScenario:
		return labelScenario, true
	case labelAfter:
		return labelAfter, true
	}
	return "", false
}

// joinBlock is a block's own text: the blank lines at each end taken off, and everything between
// them kept, because `What this touches` is read line by line later.
func joinBlock(lines []string) string {
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// refuseBadHeadings holds the rules about a step's own line: its number and its title.
//
// A duplicate names both lines, because the person fixing it has to see the one they forgot as well
// as the one they are looking at.
func refuseBadHeadings(declared []*declaredStep) error {
	seen := make(map[int32]int, len(declared))
	for _, one := range declared {
		if one.step.Number < 1 {
			return status.Errorf(codes.InvalidArgument,
				"line %d: this step is numbered %s, and a path is numbered from one",
				one.headingLine, one.numberText)
		}
		if before, already := seen[one.step.Number]; already {
			return status.Errorf(codes.InvalidArgument,
				"line %d: step %d is already declared on line %d, and two steps cannot share a number",
				one.headingLine, one.step.Number, before)
		}
		if one.step.Title == "" {
			return status.Errorf(codes.InvalidArgument,
				"line %d: step %d has no title, and a title is the one line saying what the step is",
				one.headingLine, one.step.Number)
		}
		if one.scenarioExtraLine > 0 {
			return status.Errorf(codes.InvalidArgument,
				"line %d: step %d names more than one scenario, and a step names exactly one",
				one.scenarioExtraLine, one.step.Number)
		}
		seen[one.step.Number] = one.headingLine
	}
	return nil
}

// resolveAfter reads what each step waits for, and gives a step that says nothing the number below
// it. The steps arrive in ascending number order.
//
// A default of nothing would make the gate worthless, because every step would be ready at once. So
// a numbered path is a chain unless the document says otherwise, and saying otherwise is an After
// block holding nothing.
func resolveAfter(declared []*declaredStep) error {
	numbers := make(map[int32]bool, len(declared))
	for _, one := range declared {
		numbers[one.step.Number] = true
	}

	for at, one := range declared {
		if !one.afterSaid {
			if at > 0 {
				one.step.After = declared[at-1].step.Number
			}
			continue
		}
		// An After block holding nothing means zero, which is the way to say a step waits for nobody.
		if one.afterText == "" {
			continue
		}
		read, err := strconv.Atoi(one.afterText)
		if err != nil {
			return status.Errorf(codes.InvalidArgument,
				"line %d: step %d says it comes after %q, and After takes one step number or nothing",
				one.afterLine, one.step.Number, one.afterText)
		}
		after := int32(read)
		if after == 0 {
			continue
		}
		if !numbers[after] {
			return status.Errorf(codes.InvalidArgument,
				"line %d: step %d says it comes after step %d, and this document has no step %d",
				one.afterLine, one.step.Number, after, after)
		}
		if after >= one.step.Number {
			return status.Errorf(codes.InvalidArgument,
				"line %d: step %d says it comes after step %d, and a step waits for a lower number",
				one.afterLine, one.step.Number, after)
		}
		one.step.After = after
	}
	return nil
}

// pathWarnings is what the write says about a path it kept whole. No warning refuses a document.
func pathWarnings(steps []store.Step) []string {
	var warnings []string
	if len(steps) > pathStepMark {
		warnings = append(warnings, fmt.Sprintf(
			"this path has %d steps, over the %d mark. It is kept whole.", len(steps), pathStepMark))
	}
	for _, step := range steps {
		if missing := whatTheStepDoesNotSay(step); len(missing) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"step %d says nothing under %s. It is kept as it is.",
				step.Number, strings.Join(missing, ", ")))
		}
		// Harder than the line above, because this is the one krewe itself cannot work around: it
		// runs the scenario a step names, and a step that names none is a step it cannot check.
		if step.ProofScenario == "" {
			warnings = append(warnings, fmt.Sprintf(
				"step %d names no scenario, so krewe step check will refuse this step.", step.Number))
		}
	}
	return warnings
}

// whatTheStepDoesNotSay names the blocks a step left empty, in the order the document writes them.
func whatTheStepDoesNotSay(step store.Step) []string {
	var missing []string
	for _, block := range []struct {
		label string
		text  string
	}{
		{labelIntention, step.Intention},
		{labelTouches, step.Touches},
		{labelProof, step.Proof},
		{labelScenario, step.ProofScenario},
	} {
		if block.text == "" {
			missing = append(missing, block.label)
		}
	}
	return missing
}

// SetPath replaces a project's path with the steps the document declares.
//
// A refused document changes nothing, because the parse runs before the write. There is no way to
// empty a path: a document with no step heading is refused, so a wrong file path cannot take
// somebody's path away.
func (s *Server) SetPath(ctx context.Context, req *quaycrewv1.SetPathRequest) (*quaycrewv1.SetPathResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "which project: say where with an address")
	}
	steps, warnings, err := parsePath(req.GetDocument())
	if err != nil {
		return nil, err
	}
	written, err := s.store.SetPath(ctx, req.GetProject(), steps)
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.SetPathResponse{Steps: written, Warnings: warnings}, nil
}

// ListSteps reads a project's path, or every project's when it names none.
//
// A project with no path answers with an empty list rather than an error, because nothing written is
// the normal state. Reading records nothing.
func (s *Server) ListSteps(ctx context.Context, req *quaycrewv1.ListStepsRequest) (*quaycrewv1.ListStepsResponse, error) {
	steps, err := s.store.ListSteps(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.ListStepsResponse{Steps: steps}, nil
}
