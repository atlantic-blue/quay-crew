package deploy

import (
	"os"
	"strings"
	"testing"
)

// sandboxDockerfile is the image a session runs in.
const sandboxDockerfile = "sandbox/claude.Dockerfile"

// Nothing in the image is installed at whatever a registry calls latest today.
//
// An unpinned install makes the image a moving target: the same commit builds a different thing on a
// different day, so a session that breaks cannot be told from a session whose runtime moved
// underneath it, and there is no version to go back to because none was ever written down. Every
// other tool in this image is pinned, and the model runtime was the exception.
//
// The whole class rather than the one line, so the next tool added unpinned fails here instead of
// being noticed months later.
func TestNothingInTheSandboxImageFloats(t *testing.T) {
	image := theSandboxImage(t)
	installed := everyGlobalInstall(image)
	if len(installed) == 0 {
		t.Fatal("this test found no global npm install to check, so it proves nothing")
	}
	for _, each := range installed {
		if !carriesAVersion(each) {
			t.Errorf("%q installs whatever the registry calls latest today, so this image is a "+
				"different image tomorrow. Pin it the way gh and terraform are.", each)
		}
	}
}

// carriesAVersion says whether a package name names the version to install. An npm package name can
// start with an @ of its own, as a scope, so the version's @ is any after the first character, and
// what follows it has to be something: a trailing @ with nothing after it, which is what a build
// argument declared with no default expands to, installs latest exactly like no @ at all.
func carriesAVersion(pkg string) bool {
	name, version, found := strings.Cut(strings.TrimPrefix(pkg, "@"), "@")
	return found && name != "" && version != ""
}

func theSandboxImage(t *testing.T) string {
	t.Helper()
	dockerfile, err := os.ReadFile(sandboxDockerfile)
	if err != nil {
		t.Fatalf("reading the sandbox dockerfile: %v", err)
	}
	return string(dockerfile)
}

// everyGlobalInstall returns each package a global npm install names, with any build argument
// already replaced by its default, so a package pinned through an ARG reads as pinned rather than as
// a name ending in a variable.
func everyGlobalInstall(image string) []string {
	var installed []string
	for _, line := range strings.Split(image, "\n") {
		_, after, found := strings.Cut(line, "npm install -g ")
		if !found {
			continue
		}
		for _, name := range strings.Fields(after) {
			name = strings.Trim(name, `"'`)
			// A flag, the next command in a chain, or the backslash that carries a RUN onto the next
			// line. None of them is a package, and reading the continuation as one fails this test
			// against an install that is pinned perfectly well.
			if strings.HasPrefix(name, "-") || name == "&&" || name == "\\" {
				continue
			}
			installed = append(installed, expand(image, name))
		}
	}
	return installed
}

// expand replaces a build argument reference with the argument's default.
func expand(image, name string) string {
	start := strings.Index(name, "$")
	if start < 0 {
		return name
	}
	argument := strings.Trim(name[start:], "${}")
	return name[:start] + defaultOf(image, argument)
}

// defaultOf reads what an ARG defaults to. An argument with no default expands to nothing, which
// leaves the package unpinned, so the empty answer is the honest one.
func defaultOf(image, argument string) string {
	for _, line := range strings.Split(image, "\n") {
		declared, value, found := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "ARG "), "=")
		if found && declared == argument {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
