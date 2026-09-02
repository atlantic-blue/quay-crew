// Package browser draws a page into a picture, so a session can look at what it built.
//
// A session delivers a visual change on the strength of a passing build, because it cannot see the
// page. The build, the linter, the type check and the tests all pass on a layout that is wrong, and
// the operator is the first to look at it. This is the way a session looks first.
//
// The recipe lives here rather than in each session's memory, so the decisions that make a picture
// worth having are made once: the whole page rather than the first screen of it, a viewport that is
// stated, and a wait after load so a page that draws itself in script is not caught blank.
package browser

import (
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// What a session gets for saying nothing. 1280 by 900 is a laptop, and the wait is long enough for a
// page that renders itself after load and short enough not to be felt.
const (
	DefaultWidth  = 1280
	DefaultHeight = 900
	DefaultWait   = 500 * time.Millisecond
	DefaultFile   = "render.png"
	DefaultScheme = "light"
)

// Playwright is the program the sandbox image installs, beside the headless browser it drives.
const Playwright = "playwright"

// Drawing is one picture: what to draw, how wide, in which colour scheme, and where it goes.
type Drawing struct {
	URL string
	// Shown is what to call the subject in the label under the picture. It is the url for a page, and
	// the name of the capture for a screen, because a data url carrying a whole page is not something
	// a reader can look at and say what they are being shown.
	Shown  string
	File   string
	Width  int
	Height int
	Scheme string
	Wait   time.Duration
}

// A Browser draws a Drawing, and is an interface so the behaviour specification can say what the
// system asks for without a browser to ask.
type Browser interface {
	Draw(Drawing) error
}

var (
	// viewport is a size as it is typed, 1280x900.
	viewport = regexp.MustCompile(`^([0-9]{2,5})x([0-9]{2,5})$`)
	// hasScheme says whether a url names how to fetch it. data: is matched separately, because it
	// carries no authority and so no //.
	hasScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
)

// From reads what was typed. The url comes first and everything after it is recognised by its own
// shape rather than by its position, because this tool takes no flags and four positions in a fixed
// order is a thing nobody remembers.
func From(args []string) (Drawing, error) {
	drawing := Drawing{Width: DefaultWidth, Height: DefaultHeight, Scheme: DefaultScheme, Wait: DefaultWait}
	if len(args) == 0 {
		return Drawing{}, fmt.Errorf("usage: krewe render <url> [<file>] [<width>x<height>] [light|dark] [<wait>]\n\n" +
			"for example: krewe render http://localhost:3000 home.png 390x844 dark 2s")
	}
	// A capture of a screen is drawn the same way a page is, because a product with no page still has
	// to be shown working. What was typed stays in Shown, so the line under the picture says the
	// capture it came from rather than a data url nobody can read.
	drawing.URL = address(args[0])
	drawing.Shown = drawing.URL
	if ATerminalCapture(args[0]) {
		where, err := TheCapture(args[0])
		if err != nil {
			return Drawing{}, err
		}
		drawing.URL, drawing.Shown, drawing.Scheme = where, "the screen captured in "+args[0], "dark"
	}

	named := false
	for _, arg := range args[1:] {
		switch {
		case viewport.MatchString(arg):
			size := viewport.FindStringSubmatch(arg)
			drawing.Width, _ = strconv.Atoi(size[1])
			drawing.Height, _ = strconv.Atoi(size[2])
		case arg == "light" || arg == "dark":
			drawing.Scheme = arg
		case isDuration(arg):
			drawing.Wait, _ = time.ParseDuration(arg)
		case named:
			return Drawing{}, fmt.Errorf("krewe render draws one url into one file, and %q is a second "+
				"file. A size reads as 1280x900, a colour scheme as light or dark, and a wait as 2s", arg)
		default:
			drawing.File, named = arg, true
		}
	}
	if drawing.File == "" {
		drawing.File = DefaultFile
	}
	file, err := filepath.Abs(drawing.File)
	if err != nil {
		return Drawing{}, fmt.Errorf("where to write %s: %w", drawing.File, err)
	}
	drawing.File = file
	return drawing, nil
}

// isDuration says whether a word is a length of time. A bare number is not one: 900 is far more
// likely to be half a size somebody mistyped than nine hundred nanoseconds.
func isDuration(arg string) bool {
	if _, err := strconv.Atoi(arg); err == nil {
		return false
	}
	_, err := time.ParseDuration(arg)
	return err == nil
}

// address is what a session typed, made into something a browser can fetch.
//
// A session serving a page it built types localhost:3000, which is a url whose scheme is "localhost",
// and the browser answers with something about an unsupported protocol. A bare port is the same
// mistake with less typing.
func address(typed string) string {
	switch {
	case hasScheme.MatchString(typed), strings.HasPrefix(typed, "data:"):
		return typed
	case strings.HasPrefix(typed, ":"):
		return "http://localhost" + typed
	default:
		return "http://" + typed
	}
}

// Render draws the page and says what it drew.
//
// Reading the picture back is the check as much as the label. A browser that exits well and writes
// nothing leaves a session reporting a page it never saw, which is the whole failure this package
// exists to end.
func Render(by Browser, drawing Drawing, out io.Writer) error {
	if err := by.Draw(drawing); err != nil {
		return err
	}
	width, height, err := sizeOf(drawing.File)
	if err != nil {
		return err
	}
	// The label rule 45 asks a picture to carry: what it is of, at what size, in which colour scheme,
	// and where it is. A session pastes the line under the picture and the reader knows what they
	// are looking at.
	size := ""
	if width > 0 {
		size = fmt.Sprintf(" (%d by %d)", width, height)
	}
	fmt.Fprintf(out, "drew %s at %dx%d, %s, into %s%s\n",
		drawing.Subject(), drawing.Width, drawing.Height, drawing.Scheme, drawing.File, size)
	return nil
}

// sizeOf reads back what was drawn. Only a png is read: the browser writes whatever the extension
// asks for, and a session that asked for a jpeg gets one, so this says nothing about that file
// rather than refusing it.
func sizeOf(file string) (int, int, error) {
	drawn, err := os.Open(file)
	if err != nil {
		return 0, 0, fmt.Errorf("the browser reported no error and wrote nothing to %s: %w", file, err)
	}
	defer func() { _ = drawn.Close() }()

	if !strings.EqualFold(filepath.Ext(file), ".png") {
		return 0, 0, nil
	}
	picture, err := png.DecodeConfig(drawn)
	if err != nil {
		return 0, 0, fmt.Errorf("%s is not a picture: %w", file, err)
	}
	return picture.Width, picture.Height, nil
}

// Program is a browser that is a program on this machine, which is what the sandbox image holds.
type Program struct {
	// Name is the program. Empty means the one the image installs.
	Name string
}

var _ Browser = Program{}

// Command is the whole invocation, in a function of its own so a test can read what would be run
// without a browser to run it.
//
// The whole page rather than the viewport, because a session asking what its page looks like means
// the page, and the part below the fold is where a layout defect usually is.
func (p Program) Command(drawing Drawing) *exec.Cmd {
	return exec.Command(p.name(), "screenshot",
		"--full-page",
		"--viewport-size", fmt.Sprintf("%d,%d", drawing.Width, drawing.Height),
		"--color-scheme", drawing.Scheme,
		"--wait-for-timeout", strconv.FormatInt(drawing.Wait.Milliseconds(), 10),
		drawing.URL, drawing.File,
	)
}

func (p Program) name() string {
	if p.Name == "" {
		return Playwright
	}
	return p.Name
}

// Draw runs the browser.
func (p Program) Draw(drawing Drawing) error {
	if _, err := exec.LookPath(p.name()); err != nil {
		return fmt.Errorf("this session has no browser: %s is not on the path. The sandbox image "+
			"builds one in, and a session made from an older image does not have it. Stop the session "+
			"and dispatch again to get a fresh sandbox", p.name())
	}
	said, err := p.Command(drawing).CombinedOutput()
	if err != nil {
		return fmt.Errorf("drawing %s: %w\n%s", drawing.URL, err, strings.TrimSpace(string(said)))
	}
	return nil
}

// Subject is what the label under a picture calls what was drawn.
//
// A page is its own address and needs nothing. A capture of a screen is a data url carrying the whole
// page, which is not something a reader can look at and say what they are being shown, so it is named
// by the capture it came from instead.
func (d Drawing) Subject() string {
	if d.Shown != "" {
		return d.Shown
	}
	return d.URL
}
