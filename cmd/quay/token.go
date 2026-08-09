package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/auth"
)

// crewToken is the token this tool presents on every call, or nothing when it holds none.
//
// QC_TOKEN wins, because that is how the crew hands the token to a sandbox it lets drive. After
// that the token file in the crew's data directory: QC_DATA_HOST names it when the operator moved
// it, otherwise it is under the home directory, which is where the compose stack keeps it.
//
// Holding none is not an error here: the crew's refusal says what to set, and it knows better than
// this tool whether a token is even required.
func crewToken(getenv func(string) string, read func(string) ([]byte, error)) string {
	if token := strings.TrimSpace(getenv(auth.TokenEnv)); token != "" {
		return token
	}
	dir := getenv("QC_DATA_HOST")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".quaycrew", "data")
	}
	raw, err := read(filepath.Join(dir, auth.TokenFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
