package main

import (
	"fmt"
	"io"
	"os"

	"github.com/atlantic-blue/quay-crew/internal/room"
)

// runRoom says how much memory this sandbox actually has, and what to do about a gate that does not
// fit in it. Like render, it talks to nothing: the machine a session is killed by is the one it is
// standing on, and the accounting is a file on it.
func runRoom(out io.Writer) error {
	reading, err := room.Read(os.DirFS("/"))
	if err != nil {
		return err
	}
	fmt.Fprint(out, room.Say(reading))
	return nil
}
