package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HomeEnv names the directory a system keeps everything it owns in, for a test that must not touch the
// operator's own and for an operator who keeps it somewhere else.
const HomeEnv = "KREWE_HOME"

// RetiredHomeEnv is what that variable was called before the rename. It is read for one release, so a
// shell profile, a script or a service file that sets it still points at the same directory rather
// than sending the system to a fresh one. The release that stops reading it is in the changelog.
const RetiredHomeEnv = "QUAY_HOME"

// HomeDir is the directory itself, under the operator's home, and RetiredHomeDir is what it was called
// before the rename. The command is krewe, so the directory beside it says krewe.
const (
	HomeDir        = ".krewe"
	RetiredHomeDir = ".quay"
)

// retired is where each of a system's files lived before one directory held all of them, against where
// it lives now. They are written as pairs rather than as a list of old directories because the move
// is not a rename of the parent: the data directory keeps its name one level up, and the tool's two
// files come out of a directory that is not carried across at all.
var retired = []struct {
	from  string
	to    string
	holds string
}{
	{
		from:  filepath.Join(".quaycrew", "data"),
		to:    "data",
		holds: "the tokens, the sealing key and every conversation",
	},
	{
		from:  filepath.Join(".config", "quay", "context"),
		to:    "context",
		holds: "the address you are working in",
	},
	{
		from:  filepath.Join(".config", "quay", "panel-view"),
		to:    "panel-view",
		holds: "the panel's saved view",
	},
}

// theSystemKeeps is what this directory has to hold to count as a system's own, rather than as a directory that
// happens to exist. `make config` makes the directory and writes the configuration file into it before
// anything starts, so existence alone is not the question: an empty directory beside a full retired one
// is exactly the state that comes up on a fresh token and reads as every conversation lost.
func theSystemKeeps() []string {
	names := make([]string, 0, len(retired))
	for _, one := range retired {
		names = append(names, one.to)
	}
	return names
}

// inUse says whether a system already keeps its own things here.
func inUse(dir string) bool {
	for _, name := range theSystemKeeps() {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// kreweHome is the one directory a system keeps what belongs to it on this machine: the configuration
// compose reads, the data the control plane mounts, and the tool's own files.
//
// It was three places. The data was under ~/.quaycrew, the tool's own files followed
// XDG_CONFIG_HOME into ~/.config/quay, and configuration sat in a checkout that an installed system
// does not have. Three places to find, three to back up, and no answer to "where is my system".
//
// The retired variable is read after the new one, so an operator who set it keeps their directory and
// an operator who set both gets the one they named last.
func kreweHome() (string, error) {
	for _, key := range []string{HomeEnv, RetiredHomeEnv} {
		if set := strings.TrimSpace(os.Getenv(key)); set != "" {
			return set, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find your home directory: %w", err)
	}
	return filepath.Join(home, HomeDir), nil
}

// refuseTheOldLayout stops a system whose things are still in a directory this build does not read,
// naming what to type.
//
// It refuses rather than moving anything itself. What is sitting there is a gigabyte of transcripts,
// two tokens and the key that unseals every secret, and a tool that quietly relocates those on
// startup is a tool nobody can undo. Starting anyway is worse: the system would come up empty, on a
// token nothing else holds, and every conversation would read as lost.
//
// Two shapes of move, and they are not the same command. The whole directory was renamed, which is one
// mv and no mkdir at all: making the new directory first would put the old one inside it, one level
// below anything that looks for it. The layout before that is per file, and it does need the mkdir.
func refuseTheOldLayout(home, krewe string) error {
	if inUse(krewe) {
		return nil
	}

	if renamed := filepath.Join(home, RetiredHomeDir); krewe != renamed && inUse(renamed) {
		return fmt.Errorf("this system keeps everything it owns in %s now, and %s still holds it: the "+
			"tokens, the sealing key and every conversation. Starting here would come up empty on a token "+
			"nothing else has. Move it, once, and do not make the new directory first or the old one "+
			"lands inside it:\n\n  mv %s %s", krewe, renamed, renamed, krewe)
	}

	moves := make([]string, 0, len(retired))
	for _, old := range retired {
		if _, err := os.Stat(filepath.Join(home, old.from)); err != nil {
			continue
		}
		moves = append(moves, fmt.Sprintf("  mv %s %s   # %s",
			filepath.Join(home, old.from), filepath.Join(krewe, old.to), old.holds))
	}
	if len(moves) == 0 {
		return nil
	}

	return fmt.Errorf("this system is in the layout from before %s held everything, and starting on the "+
		"new one would come up empty on a token nothing else has. Move it, once:\n\n  mkdir -p %s\n%s\n\n"+
		"then remove what is left of ~/.quaycrew and ~/.config/quay", krewe, krewe, strings.Join(moves, "\n"))
}

// theOldLayout is the startup check, reading the operator's own home directory.
func theOldLayout() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	krewe, err := kreweHome()
	if err != nil {
		return nil
	}
	return refuseTheOldLayout(home, krewe)
}
