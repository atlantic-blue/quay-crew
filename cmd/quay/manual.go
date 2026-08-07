package main

import (
	"fmt"
	"io"

	"github.com/atlantic-blue/quay-crew/internal/manual"
)

// runManual prints what quay is and how to drive it, for a session to be told with. The document
// lives in internal/manual so the scenarios can drive it.
//
//	quay manual | quay context set me/house-bills
func runManual(args []string, out io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: quay manual, and pipe it: quay manual | quay context set <address>")
	}
	fmt.Fprint(out, manual.Text(usage))
	return nil
}
