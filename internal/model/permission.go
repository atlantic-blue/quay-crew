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

// PermissionModeNamed turns what somebody typed into the mode the model understands, and says whether
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

// PermissionModeBornIn is what a thread's turns may do when it is created: the crew's own choice when
// it made one, and otherwise the mode every thread had before this was configurable.
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
