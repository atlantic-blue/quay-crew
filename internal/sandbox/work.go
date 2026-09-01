package sandbox

import (
	"os"
	"path"
	"path/filepath"
	"sort"
)

// Where a session's work is, so it can be read and published without anybody opening a container.
//
// The reason this exists: a session finished the work, wrote the file, and the job stopped telling a
// person to go into the container and push what was inside it. The bytes were never in the container
// alone. A working directory is a bind mount and a workspace's volume is another, so the system was
// holding the work the whole time and had no way to name it.

// Place is one directory a session's work could be in, in the three views that matter. They are three
// paths to one directory, and every caller here needs a different one of them: the system reads Dir,
// an operator opens Host, and a command run inside the container is given Sandbox.
type Place struct {
	// Dir is the directory as this process sees it, which is what reads a file.
	Dir string
	// Host is the directory as the machine running the sandboxes sees it, which is the path to put in
	// front of an operator. In a container the two are different paths to one place.
	Host string
	// Sandbox is where the same directory appears inside the session's container, which is what a
	// command run in there is pointed at.
	Sandbox string
}

// under is this place's child, in all three views at once, so the three cannot drift apart.
func (p Place) under(name string) Place {
	return Place{
		Dir:     filepath.Join(p.Dir, name),
		Host:    path.Join(p.Host, name),
		Sandbox: path.Join(p.Sandbox, name),
	}
}

// WorkPlaces is where this session's work could be, in the order worth looking.
//
// Two roots, because the git skill teaches two shapes and both are in use. A session takes a working
// tree of its own in the workspace's volume, which is where a repository shared by four sessions
// lives; a system that keeps no volume is told by the same brief to clone into the session's own
// directory instead. Looking in one of them would leave half the sessions unreadable.
//
// Empty where this storage keeps nothing, or where the configuration names a directory it could not
// make: there is then no work anywhere for anybody to read, which the caller has to say rather than
// paper over with a path that does not exist.
func (s Storage) WorkPlaces(cfg Config) []Place {
	dir, kept := s.WorkingDir(cfg)
	if !kept {
		return nil
	}
	host := s.Host
	if host == "" {
		host = s.Dir
	}
	places := []Place{{
		Dir:     dir,
		Host:    path.Join(host, "workspaces", cfg.Workspace, "projects", cfg.Project, "sessions", cfg.ID, "workspace"),
		Sandbox: WorkingPath,
	}}
	// The working trees this session took in the workspace's volume, named after the session the way
	// the git skill names them. Second, because a session that cloned into its own directory has
	// nothing here at all.
	if volume, held := s.VolumeDir(cfg.Workspace); held {
		places = append(places, Place{
			Dir:     filepath.Join(volume, "worktrees", cfg.ID),
			Host:    path.Join(host, "workspaces", cfg.Workspace, "volume", "worktrees", cfg.ID),
			Sandbox: path.Join(WorktreesPath, cfg.ID),
		})
	}
	return places
}

// Repository is the directory in these places that holds a git repository, and false where none does.
//
// Each place itself, then the things directly inside it. One level and no further: the git skill puts
// the checkout at the top of a working tree directory or clones into the working directory, so a
// deeper walk would find a repository somebody vendored rather than the work this session did.
//
// A `.git` that is a file counts, because that is what a working tree has: git writes a file there
// pointing back at the clone the tree came from, and refusing it would miss every session that
// followed the brief.
func Repository(places []Place) (Place, bool) {
	for _, place := range places {
		if isRepository(place.Dir) {
			return place, true
		}
		entries, err := os.ReadDir(place.Dir)
		if err != nil {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if inside := place.under(entry.Name()); isRepository(inside.Dir) {
				return inside, true
			}
		}
	}
	return Place{}, false
}

// isRepository says whether this directory is the top of a git repository or of a working tree.
func isRepository(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
