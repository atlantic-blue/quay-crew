package main

import (
	"fmt"
	"io"

	"github.com/atlantic-blue/quay-krewe/internal/manual"
)

// runManual prints what krewe is and how to drive it, for a session to be told with. The document
// lives in internal/manual so the scenarios can drive it.
//
//	krewe manual | krewe context set me/house-bills
func runManual(args []string, out io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: krewe manual, and pipe it: krewe manual | krewe context set <address>")
	}
	fmt.Fprint(out, manual.Text())
	return nil
}
