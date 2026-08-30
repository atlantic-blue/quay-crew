package main

import (
	"io"

	"github.com/atlantic-blue/krewe/internal/browser"
)

// runRender draws a url into a picture and says what it drew, so a session can look at what it built
// rather than delivering it on the strength of a passing build. The drawing itself is
// internal/browser, and it talks to nothing: the page a session wants to see is one it is serving
// inside its own sandbox.
func runRender(args []string, out io.Writer) error {
	drawing, err := browser.From(args)
	if err != nil {
		return err
	}
	return browser.Render(browser.Program{}, drawing, out)
}
