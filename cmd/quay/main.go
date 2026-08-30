// Command quay is the name this tool had before it was called krewe. It refuses, and it says what to
// type instead.
//
// Quay is Red Hat's container registry. Same audience, same vocabulary, and this tool runs
// containers, so the word had to go. What is left behind is this: an operator with the old name in
// their fingers gets one sentence naming the new one, on the first try.
//
// Leaving nothing behind is the worse of the two failures. A shell answers a missing command with
// "command not found", which reads as a broken install rather than as a rename, and nothing anywhere
// says the word moved.
//
// It refuses every invocation, whatever the arguments are. A guard that listed the commands it knew
// about would let through the one nobody remembered, and that one is always the one somebody types.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, refusal(os.Args[1:]))
	os.Exit(1)
}

// refusal is the sentence the old name answers with. It names the new command, and it names it with
// whatever was typed after it, so the line can be read once and typed back.
func refusal(args []string) string {
	typed := "krewe"
	if len(args) > 0 {
		typed += " " + args[0]
	}
	return fmt.Sprintf("quay is called krewe now, so type %q instead.", typed)
}
