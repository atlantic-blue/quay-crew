package job

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/atlantic-blue/krewe/internal/repository"
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
			"name, so write it as atlantic-blue/quay-crew", len(address), RepositoryLimit)
	}
	if !repository.Shaped(address) {
		return fmt.Errorf("job works in the repository %q, which is not an owner and a name: write it as "+
			"atlantic-blue/quay-crew, or paste the address of the repository", address)
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
	if one.Told != "" {
		return CarryOn(one)
	}
	said := []string{}
	if one.Product != "" {
		said = append(said, ServesAPerson(one.Product))
	}
	said = append(said, one.Brief)
	if one.Repository != "" {
		said = append(said, EndsInAPullRequest(one.Repository))
	}
	return strings.Join(said, "\n\n")
}

// AskedForThePullRequest is what the system sends a session that answered without one. It is the
// second half of the refusal: the job is not landed, and the session that did the work is asked for
// the one thing missing.
func AskedForThePullRequest(repository string) string {
	return fmt.Sprintf("This job works in %s and its answer named no pull request against it, so the work is "+
		"nowhere anybody can read it. Push your branch, open the pull request, and answer with its "+
		"address. Do not merge it. If you cannot push, say what stopped you.", repository)
}

// NoPullRequest is why a job that names a repository stopped without one. It is written after the
// session has been asked a second time, so it says that too: a reason that reads as though nobody
// tried sends somebody looking for a step that already happened.
func NoPullRequest(repository string) string {
	return fmt.Sprintf("this job works in %s and no answer named a pull request against it, asked twice. "+
		"The work is in the session and nowhere else: open it, and push what is there.", repository)
}
