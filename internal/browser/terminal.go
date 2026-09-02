package browser

import (
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A terminal product is drawn the same way a page is, because a person accepting work has to look at
// a picture of it either way.
//
// The browser above draws a url, and a product with no page has none. What it has instead is a
// screen, and a screen is text with colour in it: what tmux capture-pane writes with its escapes
// kept is exactly what the terminal put in front of somebody. So the capture becomes a page, and the
// browser draws that.
//
// It is a capture rather than a rendering of what the output would look like. The bytes come from a
// process that ran, so the picture is of the thing running, which is the only kind of picture the
// acceptance stage takes. A file written by hand to look like a session is a sample, and this cannot
// tell the two apart: what tells them apart is the label the person writes beside it.

// Terminal is the extensions a capture of a screen arrives in. A capture is a text file, so the
// choice is by extension rather than by content: a program's output redirected into a file has no
// shape this could recognise.
var Terminal = []string{".txt", ".ansi", ".log", ".capture", ".pane"}

// ATerminalCapture says whether this is a file of terminal output rather than an address.
//
// A file has to exist to be one. A word that looks like a path and is not there is far more likely
// to be an address somebody mistyped, and answering that with "no such file" instead of fetching it
// is the more confusing of the two failures.
func ATerminalCapture(typed string) bool {
	if hasScheme.MatchString(typed) || strings.HasPrefix(typed, "data:") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(typed))
	held := false
	for _, one := range Terminal {
		if ext == one {
			held = true
			break
		}
	}
	if !held {
		return false
	}
	found, err := os.Stat(typed)
	return err == nil && !found.IsDir()
}

// The colours a terminal has, as a browser draws them. The first eight are the ordinary ones and the
// next eight are the bright ones, taken from the palette most terminals ship with, so a picture of a
// screen looks like the screen it was taken from.
var terminalInk = []string{
	"#1c1c1c", "#d75f5f", "#5faf5f", "#d7af5f", "#5f87d7", "#af5fd7", "#5fd7d7", "#dadada",
	"#6c6c6c", "#ff8787", "#87d787", "#ffd787", "#87afff", "#d787ff", "#87ffff", "#ffffff",
}

// The page a capture is drawn on. Dark, because a terminal is, and monospaced at a size that keeps a
// screen of 200 columns inside an ordinary picture.
const (
	terminalPaper = "#0b0d10"
	terminalText  = "#d0d0d0"
	terminalFont  = "14px/1.32 'DejaVu Sans Mono','Liberation Mono','Menlo',monospace"
)

// colourEscape is one instruction about how the text after it is drawn. Everything else a terminal
// writes moves the cursor, and a capture of a screen has already had the cursor moved for it, so
// those are dropped rather than interpreted.
var colourEscape = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// otherEscape is every other escape sequence: cursor movement, clearing, mode changes, and the
// operating system commands a shell writes its title with. None of them says anything about a screen
// that has already been captured, and left in they would be drawn as text.
var otherEscape = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[\[\(][0-9;?]*[a-zA-Z]|\x1b.`)

// ink is how the text at one point in a capture is drawn.
type ink struct {
	fg, bg string
	bold   bool
}

// style is this ink as a browser reads it, and empty where the text is drawn the ordinary way.
func (i ink) style() string {
	var said []string
	if i.fg != "" {
		said = append(said, "color:"+i.fg)
	}
	if i.bg != "" {
		said = append(said, "background:"+i.bg)
	}
	if i.bold {
		said = append(said, "font-weight:700")
	}
	return strings.Join(said, ";")
}

// after is the ink these codes leave behind. A terminal carries its colour across lines until
// something changes it, so this takes what was there and returns what is there now.
func (i ink) after(codes string) ink {
	if codes == "" {
		codes = "0"
	}
	said := strings.Split(codes, ";")
	for at := 0; at < len(said); at++ {
		code, err := strconv.Atoi(said[at])
		if err != nil {
			continue
		}
		switch {
		case code == 0:
			i = ink{}
		case code == 1:
			i.bold = true
		case code == 22:
			i.bold = false
		case code >= 30 && code <= 37:
			i.fg = terminalInk[code-30]
		case code == 39:
			i.fg = ""
		case code >= 40 && code <= 47:
			i.bg = terminalInk[code-40]
		case code == 49:
			i.bg = ""
		case code >= 90 && code <= 97:
			i.fg = terminalInk[code-90+8]
		case code >= 100 && code <= 107:
			i.bg = terminalInk[code-100+8]
		case (code == 38 || code == 48) && at+2 < len(said) && said[at+1] == "5":
			i.set(code, fromTheTable(said[at+2]))
			at += 2
		case (code == 38 || code == 48) && at+4 < len(said) && said[at+1] == "2":
			i.set(code, exactly(said[at+2], said[at+3], said[at+4]))
			at += 4
		}
	}
	return i
}

// set puts a colour on the foreground or the background, whichever the code asked for.
func (i *ink) set(code int, colour string) {
	if code == 38 {
		i.fg = colour
		return
	}
	i.bg = colour
}

// fromTheTable is a colour out of the 256 a terminal numbers: sixteen ordinary ones, a cube of two
// hundred and sixteen, and a ramp of twenty four greys.
func fromTheTable(said string) string {
	number, err := strconv.Atoi(said)
	if err != nil || number < 0 || number > 255 {
		return ""
	}
	if number < 16 {
		return terminalInk[number]
	}
	if number < 232 {
		step := []int{0, 95, 135, 175, 215, 255}
		number -= 16
		return fmt.Sprintf("#%02x%02x%02x", step[number/36], step[(number/6)%6], step[number%6])
	}
	grey := 8 + (number-232)*10
	return fmt.Sprintf("#%02x%02x%02x", grey, grey, grey)
}

// exactly is a colour written as three numbers.
func exactly(red, green, blue string) string {
	r, errRed := strconv.Atoi(red)
	g, errGreen := strconv.Atoi(green)
	b, errBlue := strconv.Atoi(blue)
	if errRed != nil || errGreen != nil || errBlue != nil {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x", r&0xff, g&0xff, b&0xff)
}

// AsAPage is a capture of a screen written as a page, so the browser can draw it.
//
// The colour is kept, because a terminal says things with it that the words alone do not: a red row
// and a green row are the difference between a run that failed and one that passed, and a picture in
// grey of a screen that was not is a picture of something else.
func AsAPage(capture string) string {
	var page strings.Builder
	page.WriteString(`<!doctype html><meta charset="utf-8"><body style="margin:0;background:` +
		terminalPaper + `"><pre style="margin:0;padding:18px;color:` + terminalText + `;background:` +
		terminalPaper + `;font:` + terminalFont + `;white-space:pre">`)
	held := ink{}
	for _, line := range strings.Split(strings.ReplaceAll(capture, "\r\n", "\n"), "\n") {
		line = otherEscape.ReplaceAllString(strings.TrimRight(line, "\r"), "")
		at, open := 0, false
		for _, found := range colourEscape.FindAllStringSubmatchIndex(line, -1) {
			if at < found[0] {
				page.WriteString(html.EscapeString(line[at:found[0]]))
			}
			if open {
				page.WriteString("</span>")
				open = false
			}
			held = held.after(line[found[2]:found[3]])
			if style := held.style(); style != "" {
				page.WriteString(`<span style="` + style + `">`)
				open = true
			}
			at = found[1]
		}
		if at < len(line) {
			page.WriteString(html.EscapeString(line[at:]))
		}
		if open {
			page.WriteString("</span>")
		}
		page.WriteString("\n")
	}
	page.WriteString("</pre></body>")
	return page.String()
}

// TheCapture is a capture of a screen as an address the browser can fetch.
//
// A data url rather than a file written beside the picture, because the page is scaffolding for one
// drawing and a session that has to clean up after a render will not. The url carries the capture
// itself, so nothing outlives the draw.
func TheCapture(file string) (string, error) {
	capture, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read the capture in %s: %w", file, err)
	}
	if len(strings.TrimSpace(string(capture))) == 0 {
		return "", fmt.Errorf("%s is empty, so there is no screen in it to draw: capture the screen "+
			"while the thing is running, with tmux capture-pane -e -p, and draw that", file)
	}
	return "data:text/html;charset=utf-8," + url.PathEscape(AsAPage(string(capture))), nil
}
