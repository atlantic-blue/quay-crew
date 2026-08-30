package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HomeEnv names the directory a system keeps everything it owns in, for a test that must not touch the
// operator's own and for an operator who keeps it somewhere else.
const HomeEnv = "QUAY_HOME"

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

// quayHome is the one directory a system keeps what belongs to it on this machine: the configuration
// compose reads, the data the control plane mounts, and the tool's own files.
//
// It was three places. The data was under ~/.quaycrew, the tool's own files followed
// XDG_CONFIG_HOME into ~/.config/quay, and configuration sat in a checkout that an installed system
// does not have. Three places to find, three to back up, and no answer to "where is my system".
func quayHome() (string, error) {
	if set := strings.TrimSpace(os.Getenv(HomeEnv)); set != "" {
		return set, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find your home directory: %w", err)
	}
	return filepath.Join(home, ".quay"), nil
}

// refuseTheOldLayout stops a system that predates the move, naming what to type.
//
// It refuses rather than moving anything itself. What is sitting there is a gigabyte of transcripts,
// two tokens and the key that unseals every secret, and a tool that quietly relocates those on
// startup is a tool nobody can undo. Starting anyway is worse: the system would come up empty, on a
// token nothing else holds, and every conversation would read as lost.
func refuseTheOldLayout(home, quay string) error {
	if _, err := os.Stat(quay); err == nil {
		return nil
	}

	moves := make([]string, 0, len(retired))
	for _, old := range retired {
		if _, err := os.Stat(filepath.Join(home, old.from)); err != nil {
			continue
		}
		moves = append(moves, fmt.Sprintf("  mv %s %s   # %s",
			filepath.Join(home, old.from), filepath.Join(quay, old.to), old.holds))
	}
	if len(moves) == 0 {
		return nil
	}

	return fmt.Errorf("this system is in the layout from before %s held everything, and starting on the "+
		"new one would come up empty on a token nothing else has. Move it, once:\n\n  mkdir -p %s\n%s\n\n"+
		"then remove what is left of ~/.quaycrew and ~/.config/quay", quay, quay, strings.Join(moves, "\n"))
}

// theOldLayout is the startup check, reading the operator's own home directory.
func theOldLayout() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	quay, err := quayHome()
	if err != nil {
		return nil
	}
	return refuseTheOldLayout(home, quay)
}
