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
	"github.com/atlantic-blue/quay-krewe/internal/display"
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
	// pathFile is the path document, beside the design in the same dot directory and for the same
	// reason.
	pathFile = "path.md"
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
func (s *Server) renderDesign(ctx context.Context, session *quaycrewv1.Session, dir string, hasPath bool) string {
	design, err := s.store.GetDesign(ctx, session.GetProject())
	if err != nil {
		return ""
	}
	if design.GetBrief() == "" && design.GetBody() == "" {
		s.clearSessionFile(dir, designFile, "design")
		return ""
	}
	project, err := s.store.GetProject(ctx, session.GetProject())
	if err != nil {
		return ""
	}
	hasBody := s.writeSessionFile(dir, designFile, "design", design.GetBody())
	on, inThePath := s.stepThisSessionHolds(ctx, session)
	return designSummary(project.GetName(), design, hasBody, hasPath, on, inThePath)
}

// stepThisSessionHolds is the step this session took and how many steps the path has, and nil for a
// session that took none. Most sessions took none: an ordinary exec into a project is not a step.
//
// The path is read again here rather than handed down from the render beside it, because the summary
// and the path document are written by two calls and neither owns the other's read.
func (s *Server) stepThisSessionHolds(ctx context.Context, session *quaycrewv1.Session) (*quaycrewv1.Step, int) {
	steps, err := s.store.ListSteps(ctx, session.GetProject())
	if err != nil {
		return nil, 0
	}
	for _, step := range steps {
		if step.GetState() != store.StepTaken {
			continue
		}
		if step.GetSession() == session.GetHandle() || step.GetSession() == session.GetId() {
			return step, len(steps)
		}
	}
	return nil, len(steps)
}

// writeSessionFile puts a rendered document where the model can open it, and says whether it is
// there to open. An empty document has no file: a file that exists and says nothing costs a read.
func (s *Server) writeSessionFile(dir, name, what, body string) bool {
	if body == "" {
		s.clearSessionFile(dir, name, what)
		return false
	}
	at := filepath.Join(dir, designDir)
	if err := os.MkdirAll(at, 0o755); err != nil {
		slog.Warn("the "+what+" was not written where the session can read it", "at", at, "error", err)
		return false
	}
	if err := os.WriteFile(filepath.Join(at, name), []byte(body), 0o644); err != nil {
		slog.Warn("the "+what+" was not written where the session can read it", "at", at, "error", err)
		return false
	}
	return true
}

// clearSessionFile takes the file away when the store holds nothing, so what the session reads and
// what the store holds cannot disagree. A design or a path emptied on purpose would otherwise stay
// readable for the life of the working directory.
func (s *Server) clearSessionFile(dir, name, what string) {
	if err := os.Remove(filepath.Join(dir, designDir, name)); err != nil && !os.IsNotExist(err) {
		slog.Warn("the old "+what+" was left where the session can read it", "dir", dir, "error", err)
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
func designSummary(project string, design *quaycrewv1.Design, hasBody, hasPath bool,
	on *quaycrewv1.Step, inThePath int) string {
	lines := []string{"This project is " + project + "."}
	if hasBody {
		lines = append(lines, approvalLine(design), readLine(hasPath))
	}
	// The step this session took, which most sessions did not. It goes above the brief's own room so
	// a long brief is cut for it rather than pushing it out of the section.
	if on != nil {
		lines = append(lines, fmt.Sprintf("You are on step %d of %d: %s",
			on.GetNumber(), inThePath, on.GetTitle()))
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

// readLine sends the session to the documents it has. The path is named only when the project has
// one, for the reason renderDesign gives: a line naming a file that is not there sends the model to
// open nothing.
func readLine(hasPath bool) string {
	line := "Read " + designDir + "/" + designFile + " before you start."
	if hasPath {
		line += " The whole path is in " + designDir + "/" + pathFile + "."
	}
	return line
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

// What the session working in a project reads about the steps before its own: the path, written
// into its working directory beside the design.
//
// It is a file rather than a section in the memory file because a path grows with the project and
// the memory file is read on every exec. The summary names it, and a model that needs it opens it.

// renderPath puts the project's path where the model can open it, and says whether it is there to
// open. A project with no path has no file: a file that exists and says nothing costs a read.
//
// Nothing here fails an exec, for the same reason renderDesign fails none.
func (s *Server) renderPath(ctx context.Context, project, dir string) bool {
	steps, err := s.store.ListSteps(ctx, project)
	if err != nil {
		return false
	}
	return s.writeSessionFile(dir, pathFile, "path", pathDocument(steps))
}

// pathDocument writes the steps back out in the grammar they were read in, one block each.
//
// The order is the number's and never the store's. A session reads this file as the path, so steps
// out of order are a different path: the one thing the file is for is saying what came before this
// step.
func pathDocument(steps []*quaycrewv1.Step) string {
	if len(steps) == 0 {
		return ""
	}
	ordered := make([]*quaycrewv1.Step, len(steps))
	copy(ordered, steps)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].GetNumber() < ordered[j].GetNumber()
	})

	blocks := make([]string, 0, len(ordered))
	for _, step := range ordered {
		blocks = append(blocks, stepBlock(step))
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

// stepBlock is one step: its heading, where it stands, and the blocks the operator wrote under it,
// in the words they set. A block they left empty is left out rather than written as a bare label,
// because a label with nothing under it costs a read and answers nothing.
func stepBlock(step *quaycrewv1.Step) string {
	lines := []string{
		fmt.Sprintf("## %d. %s", step.GetNumber(), step.GetTitle()),
		"state: " + step.GetState(),
	}
	for _, block := range []struct {
		label string
		text  string
	}{
		{labelIntention, step.GetIntention()},
		{labelTouches, step.GetTouches()},
		{labelProof, step.GetProof()},
		{labelScenario, step.GetProofScenario()},
		{labelAfter, strconv.Itoa(int(step.GetAfter()))},
	} {
		if block.text == "" {
			continue
		}
		lines = append(lines, "", block.label, block.text)
	}
	return strings.Join(lines, "\n")
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

// Taking a step: the gate the operator's own command is refused by, the write that records who holds
// it, and the text the session is dispatched with.
//
// The gate reads first and starts nothing. A refusal costs one line of output, and the point of it is
// that no code exists before the operator approves the path.

// TakeStep gives one step of a project's path to a session, and dispatches that session with the step.
//
// The order is the whole design of this call. The approval is read before anything else, so a project
// whose design nobody approved never reaches the store, never mints a session and never starts a
// container. The store's own write is what refuses a step somebody already holds, because a read here
// followed by a write there would let two takes of one step both pass.
func (s *Server) TakeStep(ctx context.Context, req *quaycrewv1.TakeStepRequest) (*quaycrewv1.TakeStepResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "which project: say where with an address")
	}
	if req.GetNumber() < 1 {
		return nil, status.Error(codes.InvalidArgument, "a step number counts from one")
	}
	design, err := s.store.GetDesign(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	if !design.GetApproved() {
		return nil, status.Error(codes.FailedPrecondition,
			"this project's design is not approved, so no step can be taken. "+
				"Read it with krewe design [<address>]. Approve it with krewe design approve [<address>]")
	}

	// The whole path, because the text says which step of how many this is, and because a refusal
	// about a step nobody wrote has to say how many the path holds.
	steps, err := s.store.ListSteps(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	held := stepNumbered(steps, req.GetNumber())
	if held == nil {
		return nil, noSuchStep(req.GetNumber(), len(steps))
	}

	// The session is named before the take, because the store records who holds the step in the same
	// write that moves the state. The name is a handle, which is what a dispatch is addressed by, so
	// the session the store names is the session the dispatch continues.
	handle := store.NewID()
	taken, err := s.store.TakeStep(ctx, req.GetProject(), req.GetNumber(), handle)
	if errors.Is(err, store.ErrStepNotReady) {
		return nil, status.Error(codes.FailedPrecondition, whoHoldsIt(held))
	}
	if err != nil {
		return nil, storeError(err, "step")
	}

	text := takeText(taken, len(steps), s.projectName(ctx, req.GetProject()))
	dispatched, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: req.GetProject(), Handle: handle, Text: text, Detach: true,
	})
	if err != nil {
		return nil, err
	}
	started, err := s.store.GetSession(ctx, dispatched.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	return &quaycrewv1.TakeStepResponse{Step: taken, Session: started, Text: text}, nil
}

// stepNumbered is the step of that number, and nil when the path holds none.
func stepNumbered(steps []*quaycrewv1.Step, number int32) *quaycrewv1.Step {
	for _, step := range steps {
		if step.GetNumber() == number {
			return step
		}
	}
	return nil
}

// noSuchStep says how many steps the path has, because a number that is one past the end and a
// number nobody ever wrote read the same to whoever typed it.
func noSuchStep(number int32, held int) error {
	if held == 0 {
		return status.Errorf(codes.NotFound,
			"this project has no path, so there is no step %d to take: "+
				"write one with krewe path set [<address>] --file <path>", number)
	}
	return status.Errorf(codes.NotFound, "this path has no step %d: it has %d steps", number, held)
}

// whoHoldsIt is the refusal for a step that is not ready. It names the state, and the session where
// there is one, because the way past a step somebody is already on is to go and talk to that session.
func whoHoldsIt(step *quaycrewv1.Step) string {
	said := fmt.Sprintf("step %d is %s", step.GetNumber(), step.GetState())
	if step.GetSession() == "" {
		return said
	}
	return said + ", and session " + display.ShortID(step.GetSession()) + " holds it"
}

// takeText is what the session is given: the step whole, where it sits in the path, and what to do
// with it.
//
// A block the step left empty is left out with its label, because a label with nothing under it is
// text the model reads for nothing. The count is of the steps in the path and never of the highest
// number, so a path running 1, 2, 5 reads "of 3".
func takeText(step *quaycrewv1.Step, inThePath int, project string) string {
	blocks := []string{
		fmt.Sprintf("Step %d of %d on the path for %s.", step.GetNumber(), inThePath, project),
		step.GetTitle(),
	}
	if step.GetIntention() != "" {
		blocks = append(blocks, labelIntention+"\n"+step.GetIntention())
	}
	if step.GetTouches() != "" {
		blocks = append(blocks, labelTouches+"\n"+step.GetTouches())
	}
	if proof := proofBlock(step); proof != "" {
		blocks = append(blocks, proof)
	}
	blocks = append(blocks,
		"The design is in "+designDir+"/"+designFile+". The whole path is in "+
			designDir+"/"+pathFile+". Read both.",
		"Build this step only. Do not take work from another step.")
	return strings.Join(blocks, "\n\n") + "\n"
}

// proofBlock is what proves the step and the scenario that proves it, which are one block: the
// scenario names what the prose above it describes. Either half alone still says something, so the
// block is left out only when the step says neither.
func proofBlock(step *quaycrewv1.Step) string {
	block := ""
	if step.GetProof() != "" {
		block = labelProof + "\n" + step.GetProof()
	}
	if step.GetProofScenario() == "" {
		return block
	}
	named := "The scenario that proves it is named: " + step.GetProofScenario()
	if block == "" {
		return named
	}
	return block + "\n" + named
}

// The narrowed parts of a project: the listing, the add that gives the number, and the one line
// saying what a feature narrows to.
//
// A project delivers several features at the same time, each with its own milestones, and none of
// them waits for another. A feature carries no design and no approval of its own: those belong to
// the project, so gate 1 reads the project's design whichever feature a step sits in.

// featureTitleCap is the length past which a title is refused. A title is one line in a listing, and
// a paragraph in that column is a listing nobody can read.
//
// featureIntentionMark is the length past which an intention is long enough to say so. It refuses
// nothing: no length cap refuses text a person wrote, and the line is kept whole either way.
const (
	featureTitleCap      = 200
	featureIntentionMark = 200
)

// ListFeatures reads a project's features, or every project's when it names none.
//
// A project with no feature answers with an empty list rather than an error, because nothing written
// is the normal state. Every state comes back: filtering to the open ones is the caller's question,
// because a closed feature is what the record of the work looks like.
func (s *Server) ListFeatures(ctx context.Context, req *quaycrewv1.ListFeaturesRequest) (*quaycrewv1.ListFeaturesResponse, error) {
	features, err := s.store.ListFeatures(ctx, req.GetProject())
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.ListFeaturesResponse{Features: features}, nil
}

// AddFeature gives a project one more narrowed part of itself.
//
// The number is the store's to give, in the statement that writes the row, so two adds at one moment
// cannot take the same one. Nothing here reads the highest number, because a read here followed by a
// write there is exactly the race the store's single statement exists to close.
func (s *Server) AddFeature(ctx context.Context, req *quaycrewv1.AddFeatureRequest) (*quaycrewv1.AddFeatureResponse, error) {
	if req.GetProject() == "" {
		return nil, status.Error(codes.InvalidArgument, "which project: say where with an address")
	}
	title := req.GetTitle()
	if strings.TrimSpace(title) == "" {
		return nil, status.Error(codes.InvalidArgument,
			"this feature has no title, and a feature with no title says nothing in a listing")
	}
	if length := utf8.RuneCountInString(title); length > featureTitleCap {
		return nil, status.Errorf(codes.InvalidArgument,
			"this title is %d characters, over the %d a title holds. A title is one line naming the part of the project this feature narrows to",
			length, featureTitleCap)
	}
	feature, err := s.store.AddFeature(ctx, req.GetProject(), title)
	if err != nil {
		return nil, storeError(err, "project")
	}
	return &quaycrewv1.AddFeatureResponse{Feature: feature}, nil
}

// SetFeatureIntention records which part of the project a feature narrows to.
//
// One line, so a second line is refused rather than kept: the intention is read beside the title in a
// listing, and a paragraph there is a design in the wrong column. Length is a warning and never a
// refusal, because the text only exists in the call being made.
func (s *Server) SetFeatureIntention(ctx context.Context, req *quaycrewv1.SetFeatureIntentionRequest) (*quaycrewv1.SetFeatureIntentionResponse, error) {
	if req.GetFeature() == "" {
		return nil, status.Error(codes.InvalidArgument, "which feature: say its number")
	}
	intention := req.GetIntention()
	if strings.Contains(intention, "\n") {
		return nil, status.Error(codes.InvalidArgument,
			"an intention is one line, and this one holds a line break. Say what the feature narrows to in one line, and write the rest in the design")
	}
	feature, err := s.store.SetFeatureIntention(ctx, req.GetFeature(), intention)
	if err != nil {
		return nil, storeError(err, "feature")
	}
	return &quaycrewv1.SetFeatureIntentionResponse{
		Feature: feature,
		Warnings: overMark("feature intention", utf8.RuneCountInString(intention), featureIntentionMark,
			"one line saying which part of the project the feature narrows to"),
	}, nil
}
