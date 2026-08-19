package deploy

import (
	"strings"
	"testing"
)

// TestTheSandboxImageShipsABrowserTheSessionCanRun holds the image to the part of a browser install
// that is invisible until a session tries to use it.
//
// A browser downloads without privilege, so it lands wherever the user running the install keeps it.
// Installed as root with nothing said, that is root's home, and the session, which runs as agent,
// gets "Executable doesn't exist" with a path it cannot read. The image would build, the pin would
// look right, and the capability would be missing.
func TestTheSandboxImageShipsABrowserTheSessionCanRun(t *testing.T) {
	image := theSandboxImage(t)

	if !strings.Contains(image, "ENV PLAYWRIGHT_BROWSERS_PATH=") {
		t.Error("the image never says where browsers go, so they land in the home of whoever installed them and the sandbox user cannot read them")
	}
	if strings.Contains(image, "PLAYWRIGHT_BROWSERS_PATH=/root") {
		t.Error("the browsers are under root's home, where a session running as agent cannot reach them")
	}

	// The dependencies before the browser, because the browser install checks the host after it
	// unpacks. In this order a missing library fails the build; in the other, the check passes on a
	// machine that already had them and a session meets the missing library instead.
	deps := strings.Index(image, "playwright install-deps")
	browser := strings.Index(image, "playwright install --only-shell")
	dropped := strings.Index(image, "USER agent")
	if deps < 0 {
		t.Fatal("the image installs no browser dependencies, so the browser exits on a missing library or on a missing font configuration")
	}
	if browser < 0 {
		t.Fatal("the image installs no browser, so a session has playwright and nothing to drive")
	}
	if deps > browser {
		t.Error("the browser is installed before its dependencies, so the build cannot tell a working browser from a broken one")
	}
	if browser > dropped {
		t.Error("the browser is installed after the image drops to the sandbox user, which cannot write to the install directory")
	}
}
