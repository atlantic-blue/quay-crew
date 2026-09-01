package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/krewe/internal/workspace"
)

// contextFile is where the current address is kept, under the configuration directory. One line,
// readable and editable, the way kubectl keeps its current context: the operator should be able to
// see where they are without running anything.
const contextFile = "context"

// configHome is where the tool's own files live, which is the system's directory. It used to follow
// XDG_CONFIG_HOME into a second place of its own, so a system was two directories and a checkout.
func configHome() (string, error) {
	return kreweHome()
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
