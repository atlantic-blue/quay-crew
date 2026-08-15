package hook

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Load reads every hook in a directory, sorted by name so the order is the same everywhere.
//
// A directory with no manifest in it is not a hook and is passed over, so notes and a README can sit
// beside them. A directory with a manifest that does not make sense is an error rather than a skip:
// a hook the operator wrote and got wrong should say so, or it is simply missing later with no
// reason given, and a missing constraint is indistinguishable from a satisfied one.
func Load(dir string) ([]Hook, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("hook: read %s: %w", dir, err)
	}

	hooks := make([]Hook, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		at := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(filepath.Join(at, ManifestFile)); err != nil {
			continue
		}
		loaded, err := One(at)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, loaded)
	}
	sort.Slice(hooks, func(i, j int) bool { return hooks[i].Name < hooks[j].Name })
	return hooks, nil
}

// One reads a single hook out of its directory.
func One(dir string) (Hook, error) {
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err != nil {
		return Hook{}, fmt.Errorf("hook: read %s: %w", filepath.Join(dir, ManifestFile), err)
	}
	files, err := ReadFiles(dir)
	if err != nil {
		return Hook{}, err
	}
	loaded, err := FromFiles(files)
	if err != nil {
		return Hook{}, fmt.Errorf("%w (in %s)", err, dir)
	}
	loaded.Dir = dir
	// A hook read from a directory is named by that directory. It is the one rule that cannot travel
	// over the wire, where there is no directory to disagree with.
	if loaded.Name != filepath.Base(dir) {
		return Hook{}, fmt.Errorf("hook: %s/%s calls itself %q, and a hook is the directory it lives in",
			filepath.Base(dir), ManifestFile, loaded.Name)
	}
	return loaded, nil
}

// ReadFiles reads a directory into the files a hook is made of, carrying whether each one is
// executable. The bit is the whole reason this is not a plain read: an entry point that loses it in
// transit fails inside a container with nothing pointing back at the import.
func ReadFiles(dir string) ([]File, error) {
	var files []File
	err := filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, File{
			Path:       filepath.ToSlash(relative),
			Body:       body,
			Executable: info.Mode().Perm()&0o111 != 0,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("hook: read %s: %w", dir, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
