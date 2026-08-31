// Package forge reads back the state of a pull request the crew opened.
//
// A job that names a repository ends in a pull request, and the address lands on the row. That was
// the whole of what the crew knew about the work it opened. Nothing read the address again, so a
// change that merged and a change whose checks went red an hour later read the same: produced.
//
// Nothing here estimates, and this is the rule the whole package is built on. A reading is what a
// forge said, or it is unknown. Unknown is a word rather than a green tick, because an operator
// decides what to pick up next on these words, and a pull request that reads as passing because
// nobody could read it is the one they will not look at. The same rule the machine reading follows:
// see internal/headroom.
package forge

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// What the forge says about the pull request itself. Unknown is the answer nobody read.
const (
	StatusUnknown = "unknown"
	StatusOpen    = "open"
	StatusMerged  = "merged"
	StatusClosed  = "closed"
)

// What the checks on it say. None is a pull request with no checks at all, which is a real answer
// and not the same as one nobody read.
const (
	ChecksUnknown = "unknown"
	ChecksNone    = "none"
	ChecksPending = "pending"
	ChecksGreen   = "green"
	ChecksRed     = "red"
)

// What the reviews say. Only one of these gates a merge, and it is the one the crew has to hold.
const (
	ReviewUnknown          = "unknown"
	ReviewNone             = "none"
	ReviewApproved         = "approved"
	ReviewChangesRequested = "changes requested"
)

// A Reading is what the system last learned about one pull request.
//
// Every word has an unknown, and the zero value is unknown throughout. That is deliberate: a row
// written before this existed, a job whose reading has not come round yet and a read that the forge
// refused all say the same honest thing.
type Reading struct {
	// Status is open, merged or closed.
	Status string
	// Checks is what the checks on the head commit say together.
	Checks string
	// FailedCheck names the check that went red, and is empty for every other answer. One name
	// rather than a list: a red board is read by opening the first thing that failed.
	FailedCheck string
	// Review is what the reviews on it add up to. Changes requested is the one that stops a merge.
	Review string
	// ReadAt is when the system last read the forge, and the zero moment where it never has.
	ReadAt time.Time
	// Failed says why the reading did not happen, and is empty on a reading that did. It is kept
	// beside the unknowns so an operator reads the reason rather than guessing at it.
	Failed string
}

// Unread is what the system holds about a pull request nobody has read.
func Unread() Reading {
	return Reading{Status: StatusUnknown, Checks: ChecksUnknown, Review: ReviewUnknown}
}

// Unreadable is a reading that was attempted and did not happen, with why, stamped at the moment of
// the attempt. It is unknown throughout: a failed read must never leave the words from an older one
// standing, because a status that stopped being true is worse than no status.
func Unreadable(at time.Time, why string) Reading {
	reading := Unread()
	reading.ReadAt, reading.Failed = at, why
	return reading
}

// Taken says whether anything read this.
func (r Reading) Taken() bool { return !r.ReadAt.IsZero() && r.Failed == "" }

// Settled says the pull request has stopped moving, so nothing needs to read it again. Merged and
// closed are the two ends: everything else, unknown included, is still worth a read.
func (r Reading) Settled() bool { return r.Status == StatusMerged || r.Status == StatusClosed }

// Red says a check on this pull request failed. It is false for unknown, which is the point of the
// whole package: a reading nobody took is not a red board and it is not a green one.
func (r Reading) Red() bool { return r.Checks == ChecksRed }

// Or is the reading with its empty words filled in as unknown, which is how a row written before
// these columns existed reads.
func (r Reading) Or() Reading {
	if r.Status == "" {
		r.Status = StatusUnknown
	}
	if r.Checks == "" {
		r.Checks = ChecksUnknown
	}
	if r.Review == "" {
		r.Review = ReviewUnknown
	}
	return r
}

// String is the one line a person reads beside the address: what it is, what its checks say, and the
// name of the check that failed.
//
// A pull request nothing has read yet says so in words. "unknown" on its own reads as a reading that
// came back empty, and the two are a different problem for whoever is looking at it.
func (r Reading) String() string {
	reading := r.Or()
	if reading.ReadAt.IsZero() && reading.Failed == "" {
		return StatusUnknown + ": nothing has read it yet"
	}
	said := reading.Status
	switch {
	case reading.Checks == ChecksRed && reading.FailedCheck != "":
		said += ", checks red: " + reading.FailedCheck
	case reading.Checks == ChecksNone:
		said += ", no checks"
	default:
		said += ", checks " + reading.Checks
	}
	if reading.Review == ReviewChangesRequested {
		said += ", a review asked for changes"
	}
	if reading.Failed != "" {
		said += " (" + reading.Failed + ")"
	}
	return said
}

// An Address is the three parts of a pull request address, which is all a forge needs to be asked
// about it.
type Address struct {
	Host   string
	Owner  string
	Name   string
	Number int
}

// Repository is the owner and the name, written the way a job stores one.
func (a Address) Repository() string { return a.Owner + "/" + a.Name }

// String puts the address back together, so a reading can be reported against what was read.
func (a Address) String() string {
	return fmt.Sprintf("https://%s/%s/pull/%d", a.Host, a.Repository(), a.Number)
}

// Parse reads a pull request address. It refuses anything else rather than guessing, because the
// number it would guess is the number it would then ask a forge about.
func Parse(address string) (Address, error) {
	trimmed := strings.TrimSpace(address)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return Address{}, fmt.Errorf("forge: %q is not a pull request address", address)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" {
		return Address{}, fmt.Errorf("forge: %q is not a pull request address: it is an owner, a name, "+
			"pull, and a number", address)
	}
	number, err := strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return Address{}, fmt.Errorf("forge: %q names no pull request number", address)
	}
	return Address{Host: parsed.Host, Owner: parts[0], Name: parts[1], Number: number}, nil
}

// A Reader answers what a forge says about one pull request.
//
// It is an interface so the system can be built with no reader at all, which is what a system with
// no forge token is: it then reports unknown rather than calling a service it cannot authenticate to.
type Reader interface {
	Read(ctx context.Context, at Address) (Reading, error)
}
