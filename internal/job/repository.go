package job

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/publish"
	"github.com/atlantic-blue/quay-krewe/internal/repository"
)

// A job that names a repository ends in a pull request against it.
//
// The reason is what the operator saw: a three hour run produced one readable thing, at the end. For
// three hours the record said a session was busy and nothing else, because nothing in the tool said a
// phase ends in a push. A brief could say so, and briefs forget. So the expectation moves onto the
// job, where it is declared once and checked by the system rather than believed from the model.
//
// The check is read off the answer, the way `expect_contains` is, and for the same reason: the model
// reporting on its own job is the thing this exists to stop.

// RepositoryLimit is how long a repository address may be, which a project's repository is held to
// as well: the rule is one rule, kept in internal/repository, and named here for whoever reads a job.
const RepositoryLimit = repository.Limit

// TidyRepository is the address as it is stored: owner/name, with the spellings that arrive from a
// browser's address bar taken back down to it.
func TidyRepository(address string) string { return repository.Tidy(address) }

// usableRepository refuses an address the system could not then look for in an answer.
//
// Held to a shape at the write, while the person who typed it is looking, because the alternative is
// a job that runs for an hour and stops on an address that was never going to match anything.
func usableRepository(address string) error {
	if address == "" {
		return nil
	}
	if repository.TooLong(address) {
		return fmt.Errorf("the repository is %d bytes and the ceiling is %d: a repository is an owner and a "+
			"name, so write it as atlantic-blue/quay-krewe", len(address), RepositoryLimit)
	}
	if !repository.Shaped(address) {
		return fmt.Errorf("job works in the repository %q, which is not an owner and a name: write it as "+
			"atlantic-blue/quay-krewe, or paste the address of the repository", address)
	}
	return nil
}

// pullRequestShape is a pull request address against one repository. It is built per repository
// rather than kept as one pattern, because the point is to find the address of *this* job's pull
// request: an answer that names somebody else's pull request has not said where this work went.
//
// The host is left open. The system ships a github skill and nothing else, but the address of a pull
// request is the same three parts everywhere it matters, and pinning github.com here would refuse a
// self hosted forge for no gain.
func pullRequestShape(repository string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)https?://[^\s/]+/` + regexp.QuoteMeta(repository) + `/pull/[0-9]+`)
}

// PullRequestIn is the pull request address an answer names against this repository, and empty where
// it names none.
//
// The first one, where an answer names several. A session that opened two pull requests has done more
// than one job, and the record then points at the first of them rather than at nothing.
func PullRequestIn(repository, answer string) string {
	if repository == "" || answer == "" {
		return ""
	}
	found := pullRequestShape(repository).FindString(answer)
	// Trailing punctuation comes off. An address at the end of a sentence carries the full stop, and
	// an address in parentheses carries the bracket, and neither is part of it.
	return strings.TrimRight(found, ".,;:)]}>\"'")
}

// EndsInAPullRequest is the line a session doing this job is given beside its brief.
//
// It is added by the system rather than left to whoever wrote the brief, which is the whole change: a
// brief that forgets to ask for a push produces work nobody can see, and every brief forgets
// eventually.
//
// It says not to merge, because the merge is the gate. A push applies nothing and a merge runs the
// pipeline, so pushing early costs nothing and merging early spends money nobody approved.
func EndsInAPullRequest(repository string) string {
	return fmt.Sprintf("This job works in %s and ends in a pull request against it. Push your branch and "+
		"open the pull request before you answer, and put its address in your answer. Do not merge it: "+
		"the merge is somebody else's.", repository)
}

// Asked is the text the system sends a session for this job: the sentence the job serves where it
// carries one, the brief, and where the job names a repository, the line above.
//
// The sentence goes first because it is the frame the brief is read inside. A session given the brief
// alone builds what the brief says, which is exactly how a faithful run delivers something nobody can
// use.
//
// A job that has been told something starts again from what it was told rather than from its brief.
// The session asked a question, waited in its container, and is being started again to be given the
// answer: sending it the brief a second time would ask it to do the whole job over.
func Asked(one *Job) string {
	said := []string{}
	switch {
	// A job being continued goes first. It is the newest thing an operator decided about this job, and
	// what it was told belongs to an attempt that is over: a resume is cleared the moment the job asks
	// a question, so only one of the two is ever the instruction in hand.
	case one.Resuming != "":
		said = append(said, Continued(one))
	// A job that owes a person a plan writes the plan and nothing else. It comes before what it was
	// told, because what a planned job was told is the correction to a plan rather than the answer to
	// a question: a session given CarryOn here would be told to carry on with work it has not started.
	// It comes before a handoff for the same reason: a job with no approved plan has no work to carry
	// on with, so the session taking it over is owed the plan rather than the record.
	//
	// It is the one road that carries no line about an outcome. The answer to it is a plan, the
	// controller reads the plan and puts it to a person, and no job is ever settled on it, so a
	// session asked for a word that ends the job would be asked for something this task cannot end.
	case WaitingForItsPlan(one):
		if one.Told != "" {
			return WriteThePlanAgain(one)
		}
		return WriteThePlan(one)
	// A handoff waiting to be taken up comes next. The conversation this task is going to has never
	// seen the job, so what it gets is the brief and the record together, which is where it differs
	// from a job being continued in the conversation that did the work. See ceiling.go.
	case HandingOver(one):
		said = append(said, HandedOn(one))
	case one.Told != "":
		said = append(said, CarryOn(one))
	default:
		// A job handed to another role after it went in circles. The session reading this is not the one
		// that went round in them, and nothing it tried is in this conversation, so what those attempts
		// said is written out rather than referred to.
		if route, err := ReadRoute(one.EscalatedTo); err == nil && route.Names(RoleNow(one)) {
			said = append(said, HandedOver(one, AtStep(one.Attempted, one.LoopedStep)))
			break
		}
		if one.Product != "" {
			said = append(said, ServesAPerson(one.Product))
		}
		said = append(said, one.Brief)
		if one.Repository != "" {
			said = append(said, EndsInAPullRequest(one.Repository))
		}
		// Last, because it is the system's line about how the job is done rather than part of what it is.
		// It is here rather than in a brief because a brief that forgets it produces a job that can only
		// ever be started again from nothing.
		//
		// Where a person approved a plan, the plan carries this line with the numbers on it. Two lines
		// about recording steps, saying it two ways, is how a session ends up doing neither.
		if one.PlanApproved {
			said = append(said, FollowThePlan(one.Plan))
		} else {
			said = append(said, RecordEachStep())
		}
	}
	// Last on every road that can end the job, because it is the line the answer ends on and because
	// every one of these tasks can be the one that ends it. A task sent without it would be a session
	// held to a rule it was never given.
	said = append(said, EndsWithAnOutcome())
	return strings.Join(said, "\n\n")
}

// AskedForThePullRequest is what the system sends a session that answered without one. It is the
// second half of the refusal: the job is not landed, and the session that did the work is asked for
// the one thing missing.
func AskedForThePullRequest(repository string) string {
	return fmt.Sprintf("This job works in %s and its answer named no pull request against it, so the work is "+
		"nowhere anybody can read it. Push your branch, open the pull request, and answer with its "+
		"address. Do not merge it. If you cannot push, say what stopped you. This answer ends the job, "+
		"so state its outcome in it as well. %s", repository, EndsWithAnOutcome())
}

// NoPullRequest is why a job that names a repository stopped without one, and where its work is.
//
// It is written after the session has been asked a second time, so it says that too: a reason that
// reads as though nobody tried sends somebody looking for a step that already happened.
func NoPullRequest(repository, session string, found publish.Work) string {
	return fmt.Sprintf("this job works in %s and no answer named a pull request against it, asked twice.",
		repository) + whatBecameOfTheWork(session, found)
}

// whatBecameOfTheWork is the half of every one of these reasons an operator can act on.
//
// What it must never do is tell a person to go into a container. That is what the reason used to do,
// and it is the whole of this behaviour: the system holds the work, on a mount it made itself, so it
// either publishes the branch or it says where the bytes are. Both of those an operator can act on.
// "Open it, and push what is there" asks them to learn the layout first, and makes them the transport.
//
// Five sentences for five outcomes, and the empty one matters most. A reason that names a branch the
// session never made sends the operator looking for work that was never done.
func whatBecameOfTheWork(session string, found publish.Work) string {
	switch found.State {
	case publish.Pushed:
		if found.Pushed {
			return fmt.Sprintf(" The system pushed the branch %s, so the work is in the repository: "+
				"open the pull request from it.", found.Branch)
		}
		return fmt.Sprintf(" The branch %s is already in the repository, so the work is there: "+
			"open the pull request from it.", found.Branch)
	case publish.Held:
		return fmt.Sprintf(" The system could not push %s: %s.%s",
			branchOrIt(found.Branch), found.Why, whereItIs(session, found.Host))
	case publish.Nothing:
		return " The session committed nothing, so there is no branch to push." +
			whereItIs(session, found.Host)
	case publish.Absent:
		return " The session holds no repository." + whereItIs(session, found.Host)
	default:
		return fmt.Sprintf(" The system could not read the work: %s.%s",
			found.Why, whereItIs(session, found.Host))
	}
}

// branchOrIt names the branch, or says "it" where there is none to name. A branch nobody made must
// not appear in a sentence about pushing.
func branchOrIt(branch string) string {
	if branch == "" {
		return "it"
	}
	return "the branch " + branch
}

// whereItIs is the directory the work is in, on the machine that runs the sandboxes, and the command
// that reads it without opening anything.
//
// A system that keeps nothing on disk has no path to give, and says that rather than printing an
// empty one. It is the one case where the answer is that there is no answer, and it has to read as
// such.
func whereItIs(session, host string) string {
	if host == "" {
		return " This system keeps no working directory on disk, so there is nowhere to read it from."
	}
	return fmt.Sprintf(" The work is at %s on the machine running the sandboxes, and krewe read %s reads it.",
		host, session)
}

// A repository is reached over the network, so a job that names one needs a mode that reaches it.
//
// Every way into a repository is a command that needs the network: the clone, the push, the pull
// request. The narrower modes ask a person before they run one, and nobody stands beside a dispatched
// job, so the approval never arrives. The system held both facts at the moment of the write and never
// compared them, so it admitted the job, spent the session, and said so at the end.

// UsableModeFor refuses a job that works in a repository, in a mode somebody named that cannot reach
// the network.
//
// Read twice, from the one answer in the model layer: here for the mode and the repository a caller
// typed, and again at the control plane once the project's repository and the system's own mode have
// been filled in.
func UsableModeFor(repository, mode string) error {
	return refuseTheMode(repository, mode,
		fmt.Sprintf("this job names the mode %s", model.PermissionModeSpoken(mode)))
}

// UsableModeBornIn refuses the same pair where nobody named a mode, so the job takes the system's.
// It is the path nobody types a flag for: a project holds the repository, every job declared in it
// carries one, and the mode is whatever the system was configured with.
func UsableModeBornIn(repository, mode string) error {
	return refuseTheMode(repository, mode,
		fmt.Sprintf("this job names no mode, so it runs in the system's, which is %s",
			model.PermissionModeSpoken(mode)))
}

// refuseTheMode is the one sentence both refusals say, and the clause that says where the mode came
// from is the only thing that differs.
func refuseTheMode(repository, mode, named string) error {
	if repository == "" || model.PermissionModeReachesTheNetwork(mode) {
		return nil
	}
	return fmt.Errorf("this job works in %s, and every way into a repository needs the network: the "+
		"clone, the push and the pull request. %s, and that mode asks a person before it runs a network "+
		"command. Nobody stands beside a job, so the approval never arrives and the work stops inside "+
		"the session. Declare it with --mode %s, or leave the repository off a job that does not work "+
		"in a repository", repository, named, model.PermissionModeOnTheNetwork())
}

// WhyNoPullRequest is why a job that names a repository stopped without one, which is a different
// sentence where the mode is the reason. Both sentences end the same way, with what became of the
// work: the mode explains why nothing was pushed by the session, and it says nothing about where the
// work went.
func WhyNoPullRequest(repository, mode, session string, found publish.Work) string {
	if ModeCannotPush(mode) {
		return noPullRequestInThisMode(repository, mode, session, found)
	}
	return NoPullRequest(repository, session, found)
}

// ModeCannotPush says whether this job runs in a mode that stops it reaching its repository. A job
// that names no mode is not one: the mode it runs in is the system's, and a controller does not hold
// that, so it reads as the mode every job ran in before this was written down.
func ModeCannotPush(mode string) bool {
	return mode != "" && !model.PermissionModeReachesTheNetwork(mode)
}

// noPullRequestInThisMode says the mode is the reason, rather than sending somebody to look for a
// push that was never going to happen.
//
// The mode holds the session and not the system. A narrow mode asks a person before the session runs
// a network command, and the system's own push is not the session running anything, so the work is
// still published here: what the mode cost is the pull request, not the branch.
func noPullRequestInThisMode(repository, mode, session string, found publish.Work) string {
	return fmt.Sprintf("this job works in %s and runs in mode %s, which asks a person before it runs a "+
		"network command, so the session could never push. Nothing named a pull request against the "+
		"repository, and the session was not asked again, because the ask would have ended the same way.",
		repository, model.PermissionModeSpoken(mode)) +
		whatBecameOfTheWork(session, found) +
		fmt.Sprintf(" Declare the job again with --mode %s.", model.PermissionModeOnTheNetwork())
}
