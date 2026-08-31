package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Decide is the whole of the gate. A command line and the files this change touched go in. A refusal
// or nothing comes out.
//
// It is a pure function of those two things, so what the gate refuses is a table anybody can read and
// argue with, rather than behaviour you have to run a container to find out.
func Decide(command string, changed []string) (Refusal, bool) {
	return decide(command, changed, depth)
}

func decide(command string, changed []string, left int) (Refusal, bool) {
	if left <= 0 {
		return Refusal{}, false
	}
	for _, words := range Segments(command) {
		program, argv := Program(words)
		// A shell was handed a command line as one argument, so that argument is the command.
		if inner, isShell := ShellArgument(program, argv); isShell {
			if refusal, refused := decide(inner, changed, left-1); refused {
				return refusal, true
			}
			continue
		}
		opening, opens := Opens(program, argv)
		if !opens {
			continue
		}
		if refusal, refused := judge(opening, changed); refused {
			return refusal, true
		}
	}
	return Refusal{}, false
}

// Opening is a command that opens a pull request, and the body it would open with.
type Opening struct {
	// How names the command, in the session's own words, so the refusal quotes what was typed.
	How string
	// Body is the text of the pull request.
	Body string
	// Read says whether the body could be read at all. The --fill flag writes one from the commit
	// messages, so there is nothing on the line to read and nothing that could carry the report.
	Read bool
}

// A Refusal is what the session is told instead of what it asked for. Both halves are load bearing:
// a refusal that does not name the way through is a session that tries the next spelling of the same
// command until its budget runs out.
type Refusal struct {
	// What names the command that was refused, and why it was refused.
	What string
	// Instead is what to do rather than that.
	Instead string
}

func (r Refusal) String() string {
	return r.What + " " + r.Instead
}

// theFourSteps is the way through a refusal for a missing report. It is the deploy-identity skill in
// four lines, because a session that is refused and sent away to read a document does the shortest
// thing that gets past the gate.
const theFourSteps = "Name the identity the pipeline assumes, with krewe target <workspace>/<project>. " +
	"List every action the plan needs, resource by resource. " +
	"Ask iam:SimulatePrincipalPolicy about all of them in one call, because the first denial hides the rest: " +
	"aws iam simulate-principal-policy --policy-source-arn <identity> --action-names <action> <action>. " +
	"Then put the identity and every action you checked in the body. " +
	"Where the check cannot run at all, say that and say why, because not run is not the same as passed. " +
	"The brief is at /home/agent/skills/deploy-identity/SKILL.md."

// theReport is the way through a refusal for a reported denial. There is no way through it that opens
// a pull request, and saying so is the point.
const theReport = "A denied action stops the work being ready. " +
	"The identity cannot create what this change declares, so the report is the deliverable, and it is " +
	"worth more than the pull request it holds up. " +
	"Say which identity, which action and which resource, and what has to be granted, in your answer. " +
	"Ask about every action in one call first, so the report names the whole gap rather than the first refusal."

// judge is the rule itself, once the command is known to open a pull request.
func judge(opening Opening, changed []string) (Refusal, bool) {
	// A reported denial is refused whatever the change touches. It is the half of the rule that still
	// holds when the diff cannot be read, and the session has already done the work: it asked, it was
	// told no, and it is opening the pull request anyway.
	if opening.Read {
		if line, found := ReportsADenial(opening.Body); found {
			return Refusal{
				What: fmt.Sprintf("%s opens a pull request whose body reports a denied action: %q.",
					opening.How, line),
				Instead: theReport,
			}, true
		}
	}
	built := Infrastructure(changed)
	if len(built) == 0 {
		return Refusal{}, false
	}
	if opening.Read && CarriesTheReport(opening.Body) {
		return Refusal{}, false
	}
	missing := "its body does not name the identity that will apply it and the actions that identity needs"
	if !opening.Read {
		missing = "there is no body on the command line to carry the identity and the actions that identity needs"
	}
	return Refusal{
		What: fmt.Sprintf("%s opens a pull request that creates infrastructure (%s), and %s. "+
			"A validate and a format check never talk to the account, so green there says nothing "+
			"about whether the apply can run.", opening.How, strings.Join(built, ", "), missing),
		Instead: theFourSteps,
	}, true
}

// Opens says whether this command opens a pull request, and with what body.
//
// Two spellings, because a gate that knows one spelling is a gate the next spelling walks through:
// the command, and the same call made over the interface underneath it.
func Opens(program string, argv []string) (Opening, bool) {
	switch program {
	case "gh":
		return opensWithGh(argv)
	case "curl", "wget":
		return opensWithFetch(program, argv)
	}
	return Opening{}, false
}

func opensWithGh(argv []string) (Opening, bool) {
	bare := bareWords(argv, map[string]bool{"--repo": true, "-R": true})
	if len(bare) == 0 {
		return Opening{}, false
	}
	switch bare[0] {
	case "pr":
		if len(bare) < 2 || bare[1] != "create" {
			return Opening{}, false
		}
		body, read := bodyOfPrCreate(argv)
		return Opening{How: "`gh pr create`", Body: body, Read: read}, true
	case "api":
		if !pullsEndpoint(bare) || reads(argv) {
			return Opening{}, false
		}
		body, read := bodyOfField(argv)
		return Opening{How: "`gh api` on the pulls endpoint", Body: body, Read: read}, true
	}
	return Opening{}, false
}

// opensWithFetch catches the same call made with something that is not gh at all. The payload can be
// written many ways, so the whole command line stands as the body: what the gate asks is whether the
// identity and the actions are in there anywhere.
func opensWithFetch(program string, argv []string) (Opening, bool) {
	if !pullsEndpoint(argv) || !fetchWrites(argv) {
		return Opening{}, false
	}
	return Opening{
		How:  fmt.Sprintf("`%s` on the pulls endpoint", program),
		Body: strings.Join(argv, "\n"),
		Read: true,
	}, true
}

// bodyOfPrCreate reads the body off a gh pr create. Two ways to give one, and a third that gives
// none: --fill writes the body from the commit messages, so there is nothing here to read.
func bodyOfPrCreate(argv []string) (string, bool) {
	for at := 0; at < len(argv); at++ {
		name, value, joined := strings.Cut(argv[at], "=")
		take := func() (string, bool) {
			if joined {
				return value, true
			}
			if at+1 < len(argv) {
				at++
				return argv[at], true
			}
			return "", false
		}
		switch name {
		case "--body", "-b":
			if body, found := take(); found {
				return body, true
			}
		case "--body-file", "-F":
			path, found := take()
			if !found {
				continue
			}
			// The file is on disk beside the session, so the gate reads what the pull request would
			// carry rather than refusing a form the skill's own instructions produce. A file it
			// cannot read is a body it cannot judge.
			read, err := os.ReadFile(path)
			if err != nil {
				return "", false
			}
			return string(read), true
		}
	}
	return "", false
}

// bodyOfField reads the body out of the fields gh api sends.
func bodyOfField(argv []string) (string, bool) {
	for at := 0; at < len(argv); at++ {
		switch argv[at] {
		case "-f", "-F", "--field", "--raw-field":
			if at+1 >= len(argv) {
				continue
			}
			at++
			if name, value, found := strings.Cut(argv[at], "="); found && name == "body" {
				return value, true
			}
		}
	}
	return "", false
}

// pullsEndpoint says whether one of these words addresses the endpoint that opens a pull request:
// repos/<owner>/<repo>/pulls, with or without a host in front of it.
//
// The shape is what makes it that endpoint. Matching anything ending in /pulls would refuse a call to
// some other service that happens to spell a path the same way, and a gate that refuses work it was
// never asked to guard is a gate somebody turns off.
func pullsEndpoint(words []string) bool {
	for _, word := range words {
		address := strings.Trim(strings.Trim(word, "'\""), "/")
		parts := strings.Split(address, "/")
		if len(parts) < 4 || parts[len(parts)-1] != "pulls" {
			continue
		}
		if parts[len(parts)-4] == "repos" {
			return true
		}
	}
	return false
}

// reads says whether this gh api call is a read. gh sends GET unless it is told a method or given a
// field, so anything that says neither cannot be writing.
func reads(argv []string) bool {
	writes := map[string]bool{
		"-f": true, "-F": true, "--field": true, "--raw-field": true, "--input": true,
	}
	for at, word := range argv {
		name, value, joined := strings.Cut(word, "=")
		if name == "-X" || name == "--method" {
			if !joined && at+1 < len(argv) {
				value = argv[at+1]
			}
			return strings.EqualFold(value, "get") || strings.EqualFold(value, "head")
		}
		if writes[name] {
			return false
		}
	}
	return true
}

// fetchWrites says whether curl or wget was told to send something. Both read by default, and a read
// of the pulls endpoint lists pull requests rather than opening one.
func fetchWrites(argv []string) bool {
	sends := map[string]bool{
		"-d": true, "--data": true, "--data-raw": true, "--data-binary": true, "--json": true,
		"--post-data": true, "--post-file": true,
	}
	for at, word := range argv {
		name, value, joined := strings.Cut(word, "=")
		if sends[name] {
			return true
		}
		if name == "-X" || name == "--request" || name == "--method" {
			if !joined && at+1 < len(argv) {
				value = argv[at+1]
			}
			if strings.EqualFold(value, "post") {
				return true
			}
		}
	}
	return false
}

var (
	// An action is a service and an operation, which is how every policy and every simulator answer
	// spells one: s3:CreateBucket, dynamodb:CreateTable, iam:PassRole.
	actionPattern = regexp.MustCompile(`\b[a-z][a-z0-9-]{1,63}:[A-Z][A-Za-z0-9]{2,}\b`)
	// An identity is a role or a user, in any of the three partitions. The identity in the incident
	// was a user, so a gate that only knew roles would have let that pull request through.
	identityPattern = regexp.MustCompile(`\barn:aws[a-z-]*:iam::[0-9]{12}:(role|user)/\S+`)
)

// theSimulatorsDecisions are what iam:SimulatePrincipalPolicy answers when the identity may not do
// the thing. Both are a no: implicit means nothing grants it, explicit means something refuses it.
var theSimulatorsDecisions = []string{"implicitdeny", "explicitdeny"}

// ReportsADenial says whether the body carries an answer that came back denied, and the line it is on.
//
// One line, with an action on that same line, rather than the whole body. The two words are also how
// anybody explains what the simulator answers, and a page saying what implicitDeny means is not a
// report of one. A gate that cannot tell those apart refuses the document that teaches the rule.
func ReportsADenial(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		lowered := strings.ToLower(line)
		said := false
		for _, decision := range theSimulatorsDecisions {
			if strings.Contains(lowered, decision) {
				said = true
			}
		}
		if said && actionPattern.MatchString(line) {
			return strings.TrimSpace(line), true
		}
	}
	return "", false
}

// couldNotRun is the honest third answer: no credential in the session, a credential that may not
// call the simulator, or a cloud with no simulator at all. It is a pass here and it is a pass nowhere
// else. It puts the sentence in front of whoever merges, which is the whole of what it buys.
var couldNotRun = []string{"did not run", "could not run", "cannot run", "can not run", "was not run"}

// CarriesTheReport says whether the body says what the deploy identity may do.
//
// The identity and at least one action, because either alone is not a report: an identity with no
// actions says nothing was asked, and actions with no identity say nobody was asked about them.
func CarriesTheReport(body string) bool {
	if identityPattern.MatchString(body) && actionPattern.MatchString(body) {
		return true
	}
	lowered := strings.ToLower(body)
	for _, said := range couldNotRun {
		if strings.Contains(lowered, said) {
			return true
		}
	}
	return false
}

// Infrastructure is the files in this change that declare cloud resources, which is what makes the
// question worth asking. Terraform by name, because that is a shape a gate can be exact about.
//
// What it does not recognise is in this hook's README rather than left for somebody to discover.
func Infrastructure(changed []string) []string {
	var built []string
	for _, name := range changed {
		clean := strings.TrimSpace(name)
		if clean == "" {
			continue
		}
		if strings.HasSuffix(clean, ".tf") || strings.HasSuffix(clean, ".tf.json") {
			built = append(built, clean)
		}
	}
	// A refusal naming forty files is a refusal nobody reads to the end.
	if len(built) > 4 {
		return append(built[:4:4], fmt.Sprintf("and %d more", len(built)-4))
	}
	return built
}

// bareWords drops the flags, so what is left is the command, its subcommand and its arguments. A flag
// that takes a separate value takes its value with it, or the value reads as a command.
func bareWords(argv []string, valued map[string]bool) []string {
	bare := make([]string, 0, len(argv))
	for at := 0; at < len(argv); at++ {
		word := argv[at]
		if !strings.HasPrefix(word, "-") || word == "-" {
			bare = append(bare, word)
			continue
		}
		name, _, joined := strings.Cut(word, "=")
		if valued[name] && !joined {
			at++
		}
	}
	return bare
}

// OpensAPullRequest says whether this command line opens one at all.
//
// It is asked first, and on its own, because the answer decides whether the gate reads the change.
// This hook fires on every command a session runs, and reading a change means running git: a session
// that runs a hundred commands should pay for that on the one command this gate is about.
func OpensAPullRequest(command string) bool {
	return opensAPullRequest(command, depth)
}

func opensAPullRequest(command string, left int) bool {
	if left <= 0 {
		return false
	}
	for _, words := range Segments(command) {
		program, argv := Program(words)
		if inner, isShell := ShellArgument(program, argv); isShell {
			if opensAPullRequest(inner, left-1) {
				return true
			}
			continue
		}
		if _, opens := Opens(program, argv); opens {
			return true
		}
	}
	return false
}
