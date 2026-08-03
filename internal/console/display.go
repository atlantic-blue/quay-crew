package console

// shortIDLength is how much of an identifier a list shows. Identifiers are twelve random bytes, so
// eight hex characters is four billion values: ample for the number of workspaces and sessions one
// operator has open, and short enough that a row reads as a row rather than a wall of hex. Git and
// k9s make the same trade. The full value is what actions use and what the detail view shows.
const shortIDLength = 8

// shortID renders an identifier for a list. Anything already short enough is left alone, so this is
// safe to apply to values that are not identifiers.
func shortID(id string) string {
	if len(id) <= shortIDLength {
		return id
	}
	return id[:shortIDLength]
}

// displayName is what a list shows for something that has both a name and an identifier. The name
// is the point; the identifier is the fallback when there is no name to show, for example a session
// whose workspace has since been deleted. Neither is ever blank, because a blank cell reads as a bug.
func displayName(name, id string) string {
	if name != "" {
		return name
	}
	if id == "" {
		return "-"
	}
	return shortID(id)
}
