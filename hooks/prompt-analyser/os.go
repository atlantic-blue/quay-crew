package main

import "os"

// OS is the real machine, and the only place in the hook that touches a file.
type OS struct{}

// List names the entries of a directory, files and directories alike.
func (OS) List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names, nil
}

func (OS) Read(file string) ([]byte, error) { return os.ReadFile(file) }

func (OS) Write(file string, body []byte) error { return os.WriteFile(file, body, 0o644) }
