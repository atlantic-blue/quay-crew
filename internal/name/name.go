// Package name holds the rule for what a workspace or a project may be called.
//
// A name is half of an address. "me/house-bills" says which project of which workspace, on a command
// line, in a channel message and in a directory path on disk. So a name has to survive being typed
// without quoting, and a name containing a slash would break the address rather than sit inside it.
//
// The rule lives here rather than in the command line tool because every way in creates through the
// same control plane: the tool, the console, and every channel.
package name

import (
	"fmt"
	"strings"
	"unicode"
)

// MaxLength caps a name at something that still reads on one line next to two others.
const MaxLength = 64

// IsSlug reports whether value is lowercase letters, digits and single hyphens between them.
func IsSlug(value string) bool {
	if value == "" || len(value) > MaxLength {
		return false
	}
	previousHyphen := true // a leading hyphen is as wrong as a doubled one
	for _, r := range value {
		switch {
		case r == '-':
			if previousHyphen {
				return false
			}
			previousHyphen = true
		case unicode.IsDigit(r), r >= 'a' && r <= 'z':
			previousHyphen = false
		default:
			return false
		}
	}
	return !previousHyphen // and a trailing one
}

// Slugify tasks what someone typed into the nearest name that would be accepted, so a refusal can
// suggest something rather than only saying no. It is deliberately not applied automatically:
// quietly storing a different name than the one that was asked for is worse than refusing.
func Slugify(value string) string {
	var out strings.Builder
	previousHyphen := true
	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsDigit(r), r >= 'a' && r <= 'z':
			out.WriteRune(r)
			previousHyphen = false
		case !previousHyphen:
			out.WriteRune('-')
			previousHyphen = true
		}
	}
	trimmed := strings.Trim(out.String(), "-")
	if len(trimmed) > MaxLength {
		trimmed = strings.Trim(trimmed[:MaxLength], "-")
	}
	return trimmed
}

// Validate returns nil when value can be part of an address, and otherwise an error naming what
// would have worked. what is the thing being named, for example "workspace".
func Validate(what, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s name is required", what)
	}
	if IsSlug(value) {
		return nil
	}
	suggestion := Slugify(value)
	if suggestion == "" {
		return fmt.Errorf("%s name %q cannot be used: names are lowercase letters, digits and hyphens, for example \"house-bills\"", what, value)
	}
	return fmt.Errorf("%s name %q cannot be part of an address: use lowercase letters, digits and hyphens, for example %q", what, value, suggestion)
}

// Crew is the word an address takes to mean the level above every workspace: what the whole crew
// holds, rather than what one workspace holds. `quay skill attach crew`, `quay secret set crew` and
// `quay context set crew` all read it.
const Crew = "crew"

// ValidateWorkspace is Validate plus the one name a workspace cannot take.
//
// A workspace called "crew" would shadow the word in every address, so `quay secret set crew TOKEN`
// would set a secret on that workspace and no other workspace would ever read it. The refusal is
// here rather than in the command line tool because every way in creates through the same control
// plane.
func ValidateWorkspace(value string) error {
	if err := Validate("workspace", value); err != nil {
		return err
	}
	if strings.TrimSpace(value) == Crew {
		return fmt.Errorf("a workspace cannot be called %q: that word means the whole crew, so %q would take secrets and skills meant for every workspace", Crew, Crew)
	}
	return nil
}
