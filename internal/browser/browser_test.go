package browser

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What a session gets for saying nothing but the url. Every one of these is a decision that would
// otherwise be made again in each session, differently.
func TestADrawingIsTheWholePageAtAStatedSize(t *testing.T) {
	drawing, err := From([]string{"http://localhost:3000"})
	if err != nil {
		t.Fatalf("From: %v", err)
	}

	if drawing.Width != 1280 || drawing.Height != 900 {
		t.Errorf("the default viewport is %dx%d, and a picture whose size nobody stated says nothing", drawing.Width, drawing.Height)
	}
	if drawing.Scheme != "light" {
		t.Errorf("the default colour scheme is %q", drawing.Scheme)
	}
	if drawing.Wait != 500*time.Millisecond {
		t.Errorf("the default wait is %v, and a page that draws itself after load is caught blank without one", drawing.Wait)
	}
	if filepath.Base(drawing.File) != "render.png" {
		t.Errorf("the default file is %q", drawing.File)
	}

	argv := Program{}.Command(drawing).Args
	if !contains(argv, "--full-page") {
		t.Errorf("the browser is not asked for the whole page, so the part below the fold is never seen: %v", argv)
	}
	if !pair(argv, "--viewport-size", "1280,900") {
		t.Errorf("the viewport is not passed as the browser reads it: %v", argv)
	}
	if !pair(argv, "--wait-for-timeout", "500") {
		t.Errorf("the wait is not passed in milliseconds: %v", argv)
	}
	if !pair(argv, "--color-scheme", "light") {
		t.Errorf("the colour scheme is not passed: %v", argv)
	}
}

// A relative file is made absolute here, so the report names a path that can be opened from anywhere
// rather than one that depends on where the session happened to be standing.
func TestADrawingSaysWhereThePictureIs(t *testing.T) {
	drawing, err := From([]string{"http://localhost:3000", "home.png"})
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if !filepath.IsAbs(drawing.File) {
		t.Errorf("the file is %q, which is only a path from where the session was standing", drawing.File)
	}
}

// The words after the url are recognised by their shape, not by their position. This tool takes no
// flags, and four positions in a fixed order is a thing nobody remembers.
func TestTheWordsAfterTheUrlAreReadInAnyOrder(t *testing.T) {
	for _, order := range [][]string{
		{"http://localhost:3000", "home.png", "390x844", "dark", "2s"},
		{"http://localhost:3000", "2s", "dark", "390x844", "home.png"},
		{"http://localhost:3000", "dark", "home.png", "2s", "390x844"},
	} {
		drawing, err := From(order)
		if err != nil {
			t.Fatalf("From(%v): %v", order, err)
		}
		if filepath.Base(drawing.File) != "home.png" || drawing.Width != 390 || drawing.Height != 844 ||
			drawing.Scheme != "dark" || drawing.Wait != 2*time.Second {
			t.Errorf("%v was read as %+v", order, drawing)
		}
	}
}

// A number on its own is half a size somebody mistyped, never a length of time.
func TestABareNumberIsNotAWait(t *testing.T) {
	drawing, err := From([]string{"http://localhost:3000", "900"})
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if drawing.Wait != 500*time.Millisecond {
		t.Errorf("900 was taken as a wait of %v", drawing.Wait)
	}
	if filepath.Base(drawing.File) != "900" {
		t.Errorf("900 was not taken as the file name: %q", drawing.File)
	}
}

// A session serving what it built types the address it is serving on, and that is a url whose scheme
// is "localhost". Without this the browser reports an unsupported protocol and the session goes
// looking for the fault in its own server.
func TestTheAddressASessionActuallyTypes(t *testing.T) {
	for typed, want := range map[string]string{
		"localhost:3000":        "http://localhost:3000",
		":3000":                 "http://localhost:3000",
		"127.0.0.1:8080/health": "http://127.0.0.1:8080/health",
		"http://localhost:3000": "http://localhost:3000",
		"https://example.com":   "https://example.com",
		"file:///tmp/page.html": "file:///tmp/page.html",
		"data:text/html,<b>x":   "data:text/html,<b>x",
	} {
		if got := address(typed); got != want {
			t.Errorf("address(%q) = %q, want %q", typed, got, want)
		}
	}
}

func TestDrawingNothingSaysHowToCallIt(t *testing.T) {
	_, err := From(nil)
	if err == nil {
		t.Fatal("krewe render drew nothing and said nothing")
	}
	if !strings.Contains(err.Error(), "usage: krewe render <url>") {
		t.Errorf("the refusal does not say how to call it: %v", err)
	}
}

// One url into one file. A second file name is somebody meaning something else, and guessing which
// of the two they meant is worse than saying so.
func TestASecondFileIsRefused(t *testing.T) {
	_, err := From([]string{"http://localhost:3000", "home.png", "about.png"})
	if err == nil {
		t.Fatal("krewe render took two files without complaint")
	}
	if !strings.Contains(err.Error(), "second file") {
		t.Errorf("the refusal does not say what is wrong: %v", err)
	}
}

// The report is the label: what was drawn, at what size, in which scheme, where it landed, and how
// big the picture actually is.
func TestTheReportSaysWhatWasDrawnAndWhere(t *testing.T) {
	file := filepath.Join(t.TempDir(), "home.png")
	drawing := Drawing{URL: "http://localhost:3000", File: file, Width: 390, Height: 844, Scheme: "dark"}

	var said strings.Builder
	if err := Render(picture{width: 390, height: 1500}, drawing, &said); err != nil {
		t.Fatalf("Render: %v", err)
	}

	for _, want := range []string{"http://localhost:3000", "390x844", "dark", file, "390 by 1500"} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("the report does not say %q: %s", want, said.String())
		}
	}
}

// A browser that exits well and writes nothing is the failure this catches. Without it the session
// sees a command that worked, reports the page as checked, and has looked at nothing.
func TestAPictureThatIsNotThereIsNotReported(t *testing.T) {
	drawing := Drawing{URL: "http://localhost:3000", File: filepath.Join(t.TempDir(), "gone.png")}

	err := Render(drawsNothing{}, drawing, io.Discard)

	if err == nil {
		t.Fatal("krewe render reported a picture that was never written")
	}
	if !strings.Contains(err.Error(), "wrote nothing") {
		t.Errorf("the failure does not say what happened: %v", err)
	}
}

// And a file that is there and is not a picture.
func TestAFileThatIsNotAPictureIsNotReported(t *testing.T) {
	file := filepath.Join(t.TempDir(), "home.png")
	drawing := Drawing{URL: "http://localhost:3000", File: file}

	err := Render(writes("this is not a picture"), drawing, io.Discard)

	if err == nil {
		t.Fatal("krewe render reported a text file as a picture")
	}
	if !strings.Contains(err.Error(), "not a picture") {
		t.Errorf("the failure does not say what is wrong: %v", err)
	}
}

// A sandbox made before the image carried a browser is the shape this failure actually arrives in,
// and a session cannot install one, so the message has to name the way out.
func TestASessionWithNoBrowserIsToldWhatToDo(t *testing.T) {
	err := Program{Name: "a-browser-this-machine-does-not-have"}.Draw(Drawing{URL: "http://localhost:3000"})

	if err == nil {
		t.Fatal("a machine with no browser drew a page")
	}
	if !strings.Contains(err.Error(), "no browser") || !strings.Contains(err.Error(), "dispatch again") {
		t.Errorf("the failure does not say what to do about it: %v", err)
	}
}

// picture is a browser that writes a real one.
type picture struct{ width, height int }

func (p picture) Draw(drawing Drawing) error {
	drawn := image.NewRGBA(image.Rect(0, 0, p.width, p.height))
	drawn.Set(1, 1, color.RGBA{R: 255, A: 255})
	handle, err := os.Create(drawing.File)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	return png.Encode(handle, drawn)
}

// drawsNothing is a browser that succeeds and writes no file.
type drawsNothing struct{}

func (drawsNothing) Draw(Drawing) error { return nil }

// writes is a browser that writes something that is not a picture.
type writes string

func (w writes) Draw(drawing Drawing) error {
	return os.WriteFile(drawing.File, []byte(w), 0o600)
}

func contains(argv []string, want string) bool {
	for _, arg := range argv {
		if arg == want {
			return true
		}
	}
	return false
}

func pair(argv []string, flag, value string) bool {
	for i, arg := range argv {
		if arg == flag && i+1 < len(argv) && argv[i+1] == value {
			return true
		}
	}
	return false
}
