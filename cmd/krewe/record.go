package main

import (
	"io"

	"github.com/atlantic-blue/quay-krewe/internal/browser"
)

// runRecord joins captures of a screen into one recording and says what it recorded, so a session can
// show work whose value a still frame cannot carry. The drawing and the encoding are
// internal/browser, and neither talks to anything: the screens are ones this session captured inside
// its own sandbox.
func runRecord(args []string, out io.Writer) error {
	recording, err := browser.Recorded(args)
	if err != nil {
		return err
	}
	return browser.Record(browser.Program{}, browser.Encoder{}, recording, out)
}
