//go:build integration

package deploy

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The image runs the browser it says it runs, on the same terms as the model runtime beside it: a
// pin the registry quietly ignores reads exactly like a pin that works.
func TestTheImageRunsThePlaywrightItPins(t *testing.T) {
	image := sandboxImageUnderTest(t)
	pinned := defaultOf(theSandboxImage(t), "PLAYWRIGHT_VERSION")
	if pinned == "" {
		t.Fatal("PLAYWRIGHT_VERSION has no default, so the image installs whatever is latest today")
	}

	// The command answers "Version 1.62.1", so the version is the last word.
	reported := whatTheImageSays(t, image, "playwright", "--version")
	fields := strings.Fields(reported)
	running := fields[len(fields)-1]
	if running != pinned {
		t.Errorf("the Dockerfile pins Playwright %s and the image runs %q", pinned, reported)
	}
}

// A session in a fresh container draws a page and reads the picture back.
//
// This is the whole capability, and nothing short of it proves anything. The browser downloads
// without privilege, so an image can hold one and still be unable to start it: nineteen shared
// libraries and a font configuration stand between the two, and every one of those failures happens
// at the moment a session tries to look at its work rather than at build time.
//
// The page is deliberately taller than the viewport it is drawn at, and it carries text. So a
// picture 400 wide proves the viewport was honoured, a picture far taller than 300 proves the whole
// page was taken rather than the first screen of it, and more than one colour in it proves something
// was actually drawn rather than a blank page being handed back.
func TestASessionRendersAPageAndReadsItBack(t *testing.T) {
	image := sandboxImageUnderTest(t)

	drawn := whatTheImageDraws(t, image, `set -e
cat > /tmp/page.html <<'HTML'
<!doctype html>
<html><body style="margin:0;background:#ffffff">
  <div style="height:1200px;font:48px sans-serif;color:#101010">a session can see what it built</div>
</body></html>
HTML
quay render file:///tmp/page.html /tmp/shot.png 400x300 1>&2
cat /tmp/shot.png`)

	picture, err := png.Decode(bytes.NewReader(drawn))
	if err != nil {
		t.Fatalf("what came back is not a picture: %v", err)
	}
	bounds := picture.Bounds()
	if bounds.Dx() != 400 {
		t.Errorf("the picture is %d wide, so the viewport it was asked for was not used", bounds.Dx())
	}
	if bounds.Dy() < 1200 {
		t.Errorf("the picture is %d tall against a page of 1200, so only the first screen was taken", bounds.Dy())
	}
	if uniform(picture) {
		t.Error("every pixel is the same colour, so the browser drew a blank page and said nothing about it")
	}
}

// whatTheImageDraws runs a script in the image and returns what it wrote to standard output, which
// here is the picture itself. The command's own report goes to standard error, so a failure arrives
// with the browser's words rather than as an empty file.
func whatTheImageDraws(t *testing.T, image, script string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	command := exec.CommandContext(ctx, "docker", "run", "--rm", image, "sh", "-c", script)
	var said bytes.Buffer
	command.Stderr = &said
	drawn, err := command.Output()
	if err != nil {
		t.Fatalf("drawing a page in %s: %v\n%s", image, err, said.String())
	}
	if len(drawn) == 0 {
		t.Fatalf("nothing came back from %s\n%s", image, said.String())
	}
	return drawn
}

// uniform says whether a picture is one flat colour, which is what a browser that started, rendered
// nothing and exited well leaves behind.
func uniform(picture image.Image) bool {
	bounds := picture.Bounds()
	first := picture.At(bounds.Min.X, bounds.Min.Y)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if picture.At(x, y) != first {
				return false
			}
		}
	}
	return true
}
