package promise

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Changed reads what a change touched, by asking git.
//
// The range is base...head, which is the diff against where the branch was cut rather than against
// wherever the base has moved to since. That is the same set of files a reviewer sees on the pull
// request, so the check reads the change rather than everything that happened while it was open.
func Changed(repo, base, head string) ([]File, error) {
	// -z, because git quotes and escapes a path with a space or a quote in it in the plain form, and
	// a check that mis-reads a path is a check that asks the wrong question about it.
	command := exec.Command("git", "-C", repo, "diff", "--name-status", "-M", "-z", base+"..."+head)
	out, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		said := ""
		if errors.As(err, &exit) {
			said = ": " + strings.TrimSpace(string(exit.Stderr))
		}
		return nil, fmt.Errorf("reading what %s...%s changed%s: %w", base, head, said, err)
	}
	return parse(string(out))
}

// parse reads git's NUL separated name-status output: a status, then one path, except for a rename or
// a copy, which carry the path they came from and the path they went to.
func parse(out string) ([]File, error) {
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var files []File
	for at := 0; at < len(fields); at++ {
		code := fields[at]
		if code == "" {
			continue
		}
		switch code[0] {
		case 'R', 'C':
			if at+2 >= len(fields) {
				return nil, fmt.Errorf("git reported %q with nothing after it", code)
			}
			// The path it came from is gone and the path it went to is new, which is what a rename is
			// to everything this check asks. A feature file that only moved is not a scenario written.
			files = append(files, File{Path: fields[at+1], Status: Deleted}, File{Path: fields[at+2], Status: Added})
			at += 2
		default:
			if at+1 >= len(fields) {
				return nil, fmt.Errorf("git reported %q with no path after it", code)
			}
			files = append(files, File{Path: fields[at+1], Status: status(code)})
			at++
		}
	}
	return files, nil
}

// status turns git's letter into the word this package uses. Anything it does not know is a change to
// the file, which is the reading that asks for more rather than less.
func status(code string) Status {
	switch code[0] {
	case 'A':
		return Added
	case 'D':
		return Deleted
	default:
		return Modified
	}
}
