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

// PermissionModeNamed execs what somebody typed into the mode the model understands, and says whether
// it was a mode at all.
//
// It lives here because every surface that takes a mode needs the same answer: the command line, the
// console's wizard, and the system's own configuration. Three tables of the same five words drift, and
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

// PermissionModeBornIn is what a session's execs may do when it is created: the system's own choice when
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
	running := modeRunning(mode)
	for at, spoken := range modeOrder {
		if named, _ := PermissionModeNamed(spoken); named == running {
			return at
		}
	}
	return 0
}

// PermissionModeReachesTheNetwork says whether an exec in this mode runs a command that needs the
// network without a person approving it first.
//
// One answer in one place, because more than one surface asks it. A job that names a repository is
// held to it while the person who declared it is looking, and the controller reads it again rather
// than asking a session to push in the mode that already stopped it from pushing. Two tables of the
// same answer drift, and the drift shows up as one surface admitting what another refuses.
//
// Only the widest mode reaches it. Plan proposes and runs nothing. Edits writes the files in the
// working directory and asks a person before it runs a command, and nobody stands beside a dispatched
// job, so the approval never arrives. A mode nobody set counts as the mode a session with nothing set
// actually runs in, which is the reading PermissionModeWidens takes.
func PermissionModeReachesTheNetwork(mode string) bool {
	return modeRunning(mode) == PermissionBypass
}

// PermissionModeSpoken is the word a person types for a mode, however the mode was written down. It
// is here rather than beside each refusal so that the words a person reads in a sentence are the
// words they type back.
func PermissionModeSpoken(mode string) string {
	running := modeRunning(mode)
	for _, spoken := range modeOrder {
		if named, _ := PermissionModeNamed(spoken); named == running {
			return spoken
		}
	}
	return modeOrder[0]
}

// PermissionModeOnTheNetwork is the word for the mode that does reach the network, so a refusal says
// what to type rather than naming a mode the reader has to go and look up.
func PermissionModeOnTheNetwork() string { return PermissionModeSpoken(PermissionBypass) }

// modeRunning is the mode the runtime would actually run, however the mode was written: the word
// somebody types, the protocol's own spelling, or nothing at all, which is the mode a session with
// nothing set runs in.
func modeRunning(mode string) string {
	if named, known := PermissionModeNamed(mode); known {
		return named
	}
	return PermissionModeBornIn(mode)
}
