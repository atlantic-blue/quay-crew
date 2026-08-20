// Package display renders identifiers and names for a human reading a list, so the console and the
// command line tool shorten and label things the same way.
package display

// shortIDLength is eight hex characters of a twelve byte identifier: four billion values, and a row
// that reads as a row rather than a wall of hex. Actions use the full value.
const shortIDLength = 8

// emptyCell is what a listing prints where there is nothing to say. A blank cell reads as a bug.
const emptyCell = "-"

// ShortID is safe to apply to values that are not identifiers: anything short enough is left alone.
func ShortID(id string) string {
	if len(id) <= shortIDLength {
		return id
	}
	return id[:shortIDLength]
}

// Name prefers the name and falls back to the identifier, for example a session whose workspace has
// been deleted. Never blank: a blank cell reads as a bug.
func Name(name, id string) string {
	if name != "" {
		return name
	}
	if id == "" {
		return emptyCell
	}
	return ShortID(id)
}
