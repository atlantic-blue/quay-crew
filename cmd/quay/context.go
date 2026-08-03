package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/workspace"
)

// contextFile is where the current address is kept, under the configuration directory. One line,
// readable and editable, the way kubectl keeps its current context: the operator should be able to
// see where they are without running anything.
const contextFile = "quay/context"

// configHome is the directory configuration lives in, honouring XDG_CONFIG_HOME so a test can point
// it somewhere else and so an operator who moves their configuration is followed.
func configHome() (string, error) {
	if set := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); set != "" {
		return set, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find your home directory: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}

// currentPath reads the address the operator is working in. A missing file is not an error: it means
// they have not moved anywhere yet, and every command that needs an address says so itself.
func currentPath() (workspace.Path, error) {
	home, err := configHome()
	if err != nil {
		return workspace.Path{}, err
	}
	contents, err := os.ReadFile(filepath.Join(home, contextFile))
	if os.IsNotExist(err) {
		return workspace.Path{}, nil
	}
	if err != nil {
		return workspace.Path{}, fmt.Errorf("read the current context: %w", err)
	}
	line := strings.TrimSpace(string(contents))
	if line == "" {
		return workspace.Path{}, nil
	}
	return workspace.ParsePath(line)
}

// moveTo records where the operator is now.
func moveTo(path workspace.Path) error {
	home, err := configHome()
	if err != nil {
		return err
	}
	file := filepath.Join(home, contextFile)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(file), err)
	}
	if err := os.WriteFile(file, []byte(path.String()+"\n"), 0o644); err != nil {
		return fmt.Errorf("write the current context: %w", err)
	}
	return nil
}
