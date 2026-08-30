// Package origin says where a directory of files came from, so somebody who did not import it can go
// and read it.
//
// The crew imports a role, a skill and a hook from a directory, and a directory is anywhere. That
// makes the first import easy and everything after it invisible: an acceptance run of three hours
// was driven by three roles that sat in a folder on one machine, so no pull request touched them,
// nobody reviewed them and nothing versioned them. The clause that decided the whole outcome was
// never read by anybody but the session it was handed to.
//
// So an import records where the files came from, and the crew says it back. A role does today;
// a skill and a hook have the same hole and are not this change.
//
// Nothing here refuses an import. A role written in a scratch directory while somebody is finding
// the shape of it is ordinary, and what was missing was not a gate, it was anybody being able to
// see.
package origin

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// An Origin is where a directory's files came from, as the machine that read them saw it.
type Origin struct {
	// Repository is where the repository is, written host/owner/name. Empty when the directory is
	// not in one, or is in one with no remote.
	Repository string
	// Commit is the commit the checkout was at, in full. Empty outside a repository.
	Commit string
	// Path is the directory inside the repository, forward slashes, or the path on the machine that
	// read it when there is no repository. Empty when nothing was recorded.
	Path string
	// Dirty says the directory's own files are not what the commit holds: edited, or never committed
	// at all. The commit is then a name for something else.
	Dirty bool
	// Unpushed says the commit is on no remote branch this checkout knows about, so it is on one
	// machine however carefully it was committed.
	Unpushed bool
}

// Of reads where a directory came from.
//
// Everything it cannot read is left empty rather than guessed at. A machine with no git, a directory
// outside a repository and a repository with no remote each answer a different part of the question,
// and an Origin that filled in the rest would be inventing the one thing an operator would act on.
func Of(dir string) Origin {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		absolute = dir
	}
	top, ok := git(dir, "rev-parse", "--show-toplevel")
	if !ok {
		return Origin{Path: absolute}
	}
	read := Origin{Path: absolute}
	if inside, err := filepath.Rel(top, absolute); err == nil {
		read.Path = filepath.ToSlash(inside)
	}
	read.Commit, _ = git(dir, "rev-parse", "HEAD")
	read.Repository = Address(remote(dir))
	// Against this directory rather than the whole checkout. A change three packages away is not
	// this directory's business, and a warning that fires on every import from a working checkout is a
	// warning nobody reads.
	changed, _ := git(dir, "status", "--porcelain", "--", absolute)
	read.Dirty = strings.TrimSpace(changed) != ""
	// No commit is nothing to have pushed, and a commit on no remote branch has reached nobody.
	on, _ := git(dir, "branch", "--remotes", "--contains", "HEAD")
	read.Unpushed = strings.TrimSpace(on) == ""
	return read
}

// Reviewable says whether somebody who did not import these files could go and read them.
//
// All three, because each one on its own leaves the files somewhere nobody else can open: a
// repository nobody can name, a commit that is not what was imported, or a commit that never left
// the machine.
func (o Origin) Reviewable() bool {
	return o.Repository != "" && o.Commit != "" && !o.Dirty && !o.Unpushed
}

// Says is what to tell an operator: where the files were read from, and when nobody else could read
// them there, what to do about it.
//
// One place, because the crew says this in a listing, in an import and in whatever reads it next,
// and three copies of a sentence is three sentences that drift.
func (o Origin) Says() []string {
	said := []string{"from " + o.Line()}
	if !o.Reviewable() {
		said = append(said, "nobody else can read these files. Commit them, push them, and import again")
	}
	return said
}

// Line says where this came from and what stops anybody else reading it.
//
// Every reason, not the first one. A sentence that stops at the first sends whoever reads it to fix
// half of it and import again into the same warning.
func (o Origin) Line() string {
	if o.Path == "" && o.Repository == "" {
		return "where it came from was not recorded"
	}
	if o.Repository == "" {
		return o.Path + ", which is not in a repository"
	}
	said := o.Repository
	if o.Path != "" && o.Path != "." {
		said += " " + o.Path
	}
	if o.Commit != "" {
		said += " at " + Short(o.Commit)
	}
	var wrong []string
	if o.Dirty {
		wrong = append(wrong, "with uncommitted changes")
	}
	if o.Unpushed {
		wrong = append(wrong, "on no remote branch")
	}
	if len(wrong) > 0 {
		said += ", " + strings.Join(wrong, ", and ")
	}
	return said
}

// Short is a commit as a person writes one down.
func Short(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}

// Address is a remote written the one way, host/owner/name.
//
// Two spellings of one repository read as two places to go and look, and one of the two spellings
// carries a credential: a remote a shell rewrote to authenticate is https://<token>@github.com/...,
// and a listing that prints it puts the token on a screen and in a database.
func Address(remote string) string {
	address := strings.TrimSpace(remote)
	if address == "" {
		return ""
	}
	// scp syntax, git@github.com:owner/name.git, which is a URL in no scheme at all.
	if !strings.Contains(address, "://") {
		if host, path, found := strings.Cut(address, ":"); found && !strings.HasPrefix(address, "/") {
			address = host + "/" + path
		}
	}
	if _, after, found := strings.Cut(address, "://"); found {
		address = after
	}
	if _, after, found := strings.Cut(address, "@"); found {
		address = after
	}
	address = strings.TrimSuffix(address, "/")
	address = strings.TrimSuffix(address, ".git")
	return strings.TrimSuffix(address, "/")
}

// remote is the address this checkout pushes to: origin, or the first remote it has when it is
// called something else.
func remote(dir string) string {
	if url, ok := git(dir, "remote", "get-url", "origin"); ok && strings.TrimSpace(url) != "" {
		return url
	}
	names, ok := git(dir, "remote")
	if !ok {
		return ""
	}
	for _, name := range strings.Fields(names) {
		if url, ok := git(dir, "remote", "get-url", name); ok {
			return url
		}
	}
	return ""
}

// git runs one read against a checkout. It says whether it could, so a machine with no git at all
// answers "not in a repository" rather than crashing an import.
func git(dir string, args ...string) (string, bool) {
	command := exec.Command("git", args...)
	command.Dir = dir
	out, err := command.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
