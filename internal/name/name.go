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

// System is the word an address takes to mean the level above every workspace: what the whole system
// holds, rather than what one workspace holds. `krewe skill attach system`, `krewe secret set system` and
// `krewe context set system` all read it.
const System = "system"

// Retired is the word that meant this level until it became System.
//
// It is refused by name wherever the new word is taken, rather than falling through to be read as
// the name of a workspace that cannot exist. A word that stops working quietly is the worse of the
// two failures: `krewe secret set crew CLAUDE_CODE_OAUTH_TOKEN`, typed out of habit, would come back
// saying there is no such workspace, which sends the operator looking for a workspace instead of
// telling them the one thing they need, which is what to type instead.
const Retired = "crew"

// RefuseRetired returns the refusal for the word this level used to take, and nil for anything else,
// so a caller puts it in front of whatever it does with an address and nothing else changes.
func RefuseRetired(typed string) error {
	if strings.TrimSpace(typed) != Retired {
		return nil
	}
	return fmt.Errorf("%q is not a word this takes any more: the level above every workspace is called %q, so type %q", Retired, System, System)
}

// ValidateWorkspace is Validate plus the one name a workspace cannot take.
//
// A workspace called "system" would shadow the word in every address, so `krewe secret set system TOKEN`
// would set a secret on that workspace and no other workspace would ever read it. The refusal is
// here rather than in the command line tool because every way in creates through the same control
// plane.
func ValidateWorkspace(value string) error {
	if err := Validate("workspace", value); err != nil {
		return err
	}
	if strings.TrimSpace(value) == System {
		return fmt.Errorf("a workspace cannot be called %q: that word means the whole system, so %q would take secrets and skills meant for every workspace", System, System)
	}
	// The word that used to mean the level stays reserved. A workspace holding it would be handed
	// everything typed out of habit, quietly, and nothing anywhere would say that the word had moved.
	if strings.TrimSpace(value) == Retired {
		return fmt.Errorf("a workspace cannot be called %q: that word used to mean the level above every workspace, so `krewe secret set %s` typed out of habit would land here. The word is now %q", Retired, Retired, System)
	}
	return nil
}
