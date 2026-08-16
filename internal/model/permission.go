package model

import "strings"

// modeOrder is the modes in the order they widen, narrowest first, so a list that offers them never
// puts the most permissive one under the cursor.
var modeOrder = []string{"plan", "edits", "dangerous"}

// spokenModes are the words somebody types against the mode the model understands.
//
// The console's listing has always printed "edits" and "dangerous", so those are what a person reads
// before they type anything. The protocol's own spellings are taken too, because they are what the
// manual prints and what a script written against the protocol would send.
//
// Keyed lowercase; PermissionModeNamed folds what it is given.
var spokenModes = map[string]string{
	"plan":              PermissionPlan,
	"edits":             PermissionAcceptEdits,
	"acceptedits":       PermissionAcceptEdits,
	"dangerous":         PermissionBypass,
	"bypasspermissions": PermissionBypass,
}

// PermissionModeNamed tasks what somebody typed into the mode the model understands, and says whether
// it was a mode at all.
//
// It lives here because every surface that takes a mode needs the same answer: the command line, the
// console's wizard, and the crew's own configuration. Three tables of the same five words drift, and
// the drift shows up as one surface taking a word another refuses.
func PermissionModeNamed(spoken string) (string, bool) {
	mode, known := spokenModes[strings.ToLower(strings.TrimSpace(spoken))]
	return mode, known
}

// PermissionModesOffered is the modes as a person types them, narrowest first, for a refusal that has
// to say what the choices are.
func PermissionModesOffered() []string {
	return append([]string(nil), modeOrder...)
}

// PermissionModeBornIn is what a session's tasks may do when it is created: the crew's own choice when
// it made one, and otherwise the mode every session had before this was configurable.
//
// The fallback is here rather than at each caller so a store, a server and a test cannot each pick a
// different one. An unknown value falls back rather than failing, because the place that refuses a
// wrong one is the startup that reads it, where the operator is standing and can fix it.
func PermissionModeBornIn(configured string) string {
	if KnownPermissionMode(configured) {
		return configured
	}
	return PermissionAcceptEdits
}

// PermissionModeWidens says whether moving from one mode to another gives the model more room.
//
// The modes are ordered, narrowest first, so this is a comparison of where each sits: plan reads and
// proposes, edits changes files in one directory, dangerous does anything. A surface that asks before
// it widens and not before it narrows needs to know which way a change goes, and computing that from
// the order rather than listing the pairs means a fourth mode cannot be added without it.
//
// An unknown mode counts as the mode a session with nothing set actually runs in, which is what makes
// a session from before the mode was written down compare like every other one.
func PermissionModeWidens(from, to string) bool {
	return permissionRank(to) > permissionRank(from)
}

func permissionRank(mode string) int {
	spelt := PermissionModeBornIn(mode)
	for at, spoken := range modeOrder {
		if named, _ := PermissionModeNamed(spoken); named == spelt {
			return at
		}
	}
	return 0
}
