package browser

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captured writes a capture of a screen, which is what a recording is made of.
func captured(t *testing.T, dir, name, said string) string {
	t.Helper()
	where := filepath.Join(dir, name)
	if err := os.WriteFile(where, []byte(said), 0o600); err != nil {
		t.Fatalf("write a capture: %v", err)
	}
	return where
}

// draws is a browser that writes a frame for every capture it is given, and keeps what it was asked
// for so a test can say the recording was made of the captures rather than of one of them.
type draws struct {
	asked []Drawing
}

func (d *draws) Draw(drawing Drawing) error {
	d.asked = append(d.asked, drawing)
	return os.WriteFile(drawing.File, []byte("a frame"), 0o600)
}

// joins is an encoder that writes a recording of the frames it was handed.
type joins struct {
	frames []string
	into   Recording
}

func (j *joins) Join(frames []string, into Recording) error {
	j.frames, j.into = frames, into
	return os.WriteFile(into.File, []byte("a recording"), 0o600)
}

// joinsNothing is the encoder that exits well and writes nothing, which reads as a recording
// everywhere a session looks.
type joinsNothing struct{}

func (joinsNothing) Join([]string, Recording) error { return nil }

// What a recording gets for saying nothing but the file and the captures.
func TestARecordingIsEveryCaptureAtAStatedRate(t *testing.T) {
	recording, err := Recorded([]string{"run.webm", "frame-1.txt", "frame-2.txt"})
	if err != nil {
		t.Fatalf("Recorded: %v", err)
	}
	if len(recording.Captures) != 2 {
		t.Errorf("the recording is made of %d captures and 2 were named", len(recording.Captures))
	}
	if recording.Frames != DefaultFrames {
		t.Errorf("the rate is %d a second and the default is %d", recording.Frames, DefaultFrames)
	}
	if recording.Scheme != "dark" {
		t.Errorf("a screen is drawn %s, and a terminal is dark", recording.Scheme)
	}
	if !filepath.IsAbs(recording.File) {
		t.Errorf("the recording goes to %q, which is not somewhere a person can find it", recording.File)
	}
}

// The order is the order the captures were named in. A recording whose frames are reordered shows
// something that never happened.
func TestTheFramesStayInTheOrderTheyWereCapturedIn(t *testing.T) {
	recording, err := Recorded([]string{"run.webm", "z-last.txt", "a-first.txt", "m-middle.txt"})
	if err != nil {
		t.Fatalf("Recorded: %v", err)
	}
	want := []string{"z-last.txt", "a-first.txt", "m-middle.txt"}
	for at, one := range want {
		if recording.Captures[at] != one {
			t.Fatalf("frame %d is %q and it was captured as %q: %v",
				at+1, recording.Captures[at], one, recording.Captures)
		}
	}
}

// The words after the file are read by their shape, the way krewe render already reads them.
func TestTheWordsAfterTheFileAreReadInAnyOrder(t *testing.T) {
	recording, err := Recorded([]string{"run.webm", "900x400", "one.txt", "4", "light", "two.txt"})
	if err != nil {
		t.Fatalf("Recorded: %v", err)
	}
	if recording.Width != 900 || recording.Height != 400 {
		t.Errorf("the size is %dx%d and 900x400 was typed", recording.Width, recording.Height)
	}
	if recording.Frames != 4 {
		t.Errorf("the rate is %d a second and 4 was typed", recording.Frames)
	}
	if recording.Scheme != "light" {
		t.Errorf("the scheme is %q and light was typed", recording.Scheme)
	}
	if len(recording.Captures) != 2 {
		t.Errorf("the recording is made of %d captures and 2 were named: %v",
			len(recording.Captures), recording.Captures)
	}
}

// Recording nothing says how to record something.
func TestRecordingNothingSaysHowToCaptureAScreen(t *testing.T) {
	_, err := Recorded(nil)
	if err == nil {
		t.Fatal("krewe record took no captures and refused nothing")
	}
	for _, want := range []string{"usage: krewe record", "capture-pane"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the usage does not say %q: %v", want, err)
		}
	}
}

// One frame is a picture. Saying so, rather than writing a recording that never moves, is what stops
// a session offering a still under the name of a recording.
func TestOneFrameIsAPictureRatherThanARecording(t *testing.T) {
	_, err := Recorded([]string{"run.webm", "frame-1.txt"})
	if err == nil {
		t.Fatal("one capture was accepted as a recording")
	}
	if !strings.Contains(err.Error(), "krewe render") {
		t.Errorf("the refusal does not say what to do with one capture: %v", err)
	}
}

// The encoder this system has writes webm and nothing else, so a file named for a format it cannot
// write is refused before anything is drawn rather than after every frame.
func TestARecordingIsNamedForTheFormatTheEncoderWrites(t *testing.T) {
	_, err := Recorded([]string{"run.mp4", "one.txt", "two.txt"})
	if err == nil {
		t.Fatal("krewe record accepted a name it cannot write")
	}
	if !strings.Contains(err.Error(), "webm") {
		t.Errorf("the refusal does not name what it writes: %v", err)
	}
}

// Every capture becomes a frame, and the frames are jpeg: the encoder beside the browser decodes
// mjpeg and not png, so a recording drawn as png carries no video stream at all.
func TestEveryCaptureIsDrawnAsAJpegFrame(t *testing.T) {
	dir := t.TempDir()
	recording := Recording{
		Captures: []string{
			captured(t, dir, "one.txt", "first screen"),
			captured(t, dir, "two.txt", "second screen"),
			captured(t, dir, "three.txt", "third screen"),
		},
		File: filepath.Join(dir, "run.webm"), Width: 900, Height: 400, Scheme: "dark", Frames: 2,
	}

	browser, encoder := &draws{}, &joins{}
	var said strings.Builder
	if err := Record(browser, encoder, recording, &said); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if len(browser.asked) != 3 {
		t.Fatalf("%d frames were drawn and 3 screens were captured", len(browser.asked))
	}
	for at, drawing := range browser.asked {
		if filepath.Ext(drawing.File) != ".jpg" {
			t.Errorf("frame %d is %q, and the encoder reads mjpeg", at+1, drawing.File)
		}
		if drawing.Width != 900 || drawing.Scheme != "dark" {
			t.Errorf("frame %d was drawn at %dx%d %s, and the recording says 900x400 dark",
				at+1, drawing.Width, drawing.Height, drawing.Scheme)
		}
	}
	if len(encoder.frames) != 3 {
		t.Errorf("the encoder was handed %d frames and 3 were drawn", len(encoder.frames))
	}
	for _, want := range []string{"3 captures", "900x400", "1.5 seconds", "run.webm"} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("the report does not say %q: %s", want, said.String())
		}
	}
}

// The frames are the recording's working out, and a session that finds forty of them beside its work
// cannot tell them from what it built.
func TestTheFramesAreGoneAndTheRecordingIsNot(t *testing.T) {
	dir := t.TempDir()
	recording := Recording{
		Captures: []string{captured(t, dir, "one.txt", "a"), captured(t, dir, "two.txt", "b")},
		File:     filepath.Join(dir, "run.webm"), Frames: 2,
	}

	browser, encoder := &draws{}, &joins{}
	if err := Record(browser, encoder, recording, io.Discard); err != nil {
		t.Fatalf("Record: %v", err)
	}

	for _, frame := range encoder.frames {
		if _, err := os.Stat(frame); err == nil {
			t.Errorf("%s is still there after the recording was made", frame)
		}
	}
	if _, err := os.Stat(recording.File); err != nil {
		t.Errorf("the recording is not there: %v", err)
	}
}

// An encoder that exits well and writes nothing leaves a session reporting a recording nobody can
// watch, which is the still picture's failure arriving in a moving one.
func TestARecordingThatIsNotThereIsNotReported(t *testing.T) {
	dir := t.TempDir()
	recording := Recording{
		Captures: []string{captured(t, dir, "one.txt", "a"), captured(t, dir, "two.txt", "b")},
		File:     filepath.Join(dir, "gone.webm"), Frames: 2,
	}

	err := Record(&draws{}, joinsNothing{}, recording, io.Discard)

	if err == nil {
		t.Fatal("krewe record reported a recording that was never written")
	}
	if !strings.Contains(err.Error(), "wrote nothing") {
		t.Errorf("the failure does not say what happened: %v", err)
	}
}

// A capture that is not there is the shape of an address somebody mistyped, and the refusal comes
// before anything is drawn.
func TestACaptureThatIsNotThereStopsTheRecording(t *testing.T) {
	dir := t.TempDir()
	recording := Recording{
		Captures: []string{captured(t, dir, "one.txt", "a"), filepath.Join(dir, "gone.txt")},
		File:     filepath.Join(dir, "run.webm"), Frames: 2,
	}

	err := Record(&draws{}, &joins{}, recording, io.Discard)

	if err == nil {
		t.Fatal("a capture that is not there was recorded")
	}
}

// The sad path this system has to state plainly: a machine with no encoder cannot record, and what a
// session does next is attach a recording made somewhere else.
func TestASystemWithNoEncoderSaysItCannotRecord(t *testing.T) {
	t.Setenv(FfmpegVariable, filepath.Join(t.TempDir(), "no-such-encoder"))

	_, err := TheEncoder("")

	if err == nil {
		t.Fatal("an encoder that is not there was found")
	}
	if !strings.Contains(err.Error(), "cannot record") {
		t.Errorf("the refusal does not say this system cannot record: %v", err)
	}
	if !strings.Contains(err.Error(), "attach") {
		t.Errorf("the refusal does not say what to do instead: %v", err)
	}
}

// The encoder is found rather than named, because the folder it arrives in carries a version that
// moves with the sandbox image.
func TestTheEncoderIsFoundBesideTheBrowser(t *testing.T) {
	found, err := TheEncoder("")
	if err != nil {
		if !strings.Contains(err.Error(), "cannot record") {
			t.Errorf("the refusal does not say this system cannot record: %v", err)
		}
		return
	}
	if held, err := os.Stat(found); err != nil || held.IsDir() {
		t.Errorf("TheEncoder found %q, which is not a program: %v", found, err)
	}
}

// The invocation, read without an encoder to run it. Every one of these is a decision the encoder
// beside the browser forces: it has no image2 demuxer, so the frames arrive on a pipe, and it does
// not probe what they are, so the stream is named.
func TestTheEncoderIsToldTheFramesAreJpegOnAPipe(t *testing.T) {
	command := Encoder{Name: "ffmpeg"}.Command(
		[]string{"frame-0000.jpg"},
		Recording{File: "/tmp/run.webm", Frames: 4},
	)

	said := strings.Join(command.Args, " ")
	for _, want := range []string{"-f image2pipe", "-c:v mjpeg", "-i pipe:0", "-framerate 4",
		"-c:v vp8", "/tmp/run.webm"} {
		if !strings.Contains(said, want) {
			t.Errorf("the encoder is not told %q: %s", want, said)
		}
	}
}
