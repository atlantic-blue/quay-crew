package browser

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// A moving picture is drawn the same way a still one is, one frame at a time.
//
// Some value a still frame cannot carry: a list that refreshes, a wizard that comes back to where it
// started, a key that is swallowed. A person asked to accept that work needs to watch it happen, so
// the acceptance stage takes a recording as well as a picture.
//
// Nothing in this sandbox records a terminal. There is no asciinema, no ttyrec, no vhs and no ffmpeg
// on the path. What there is arrives with the headless browser: the encoder Playwright writes its own
// videos with, which reads frames on a pipe and writes one webm. So a recording here is made of
// captures, each drawn by the browser above, and joined by that encoder.
//
// The frames are jpeg rather than png, and that is not a preference. The encoder is built with the
// mjpeg decoder and without the png one, so a pipe of png frames opens and carries no video stream.

// What a recording gets for saying nothing. Two frames a second is a screen somebody is reading
// rather than a film, and it keeps a recording of a minute under a hundred frames.
const (
	DefaultFrames    = 2
	DefaultRecording = "recording.webm"
	// FfmpegVariable names the encoder, for a machine that holds it somewhere this does not look.
	FfmpegVariable = "KREWE_FFMPEG"
	// Encoders is where the encoder sits when it arrives with the headless browser. The version in the
	// folder moves with the image, so it is found rather than named.
	Encoders = "/opt/playwright/ffmpeg-*/ffmpeg-*"
)

// Recording is one recording: the captures it is made of, in order, and where it goes.
type Recording struct {
	// Captures are the screens, oldest first. Each one is a file of terminal output, the same shape a
	// still picture is drawn from.
	Captures []string
	File     string
	Width    int
	Height   int
	Scheme   string
	// Frames is how many of the captures are shown each second.
	Frames int
}

// Recorder joins drawn frames into one recording, and is an interface so the behaviour specification
// can say what the system asks for without an encoder to ask.
type Recorder interface {
	Join(frames []string, into Recording) error
}

// Recorded reads what was typed. The file comes first here and the captures follow, because the
// captures are a list: with the file last, a glob that matches nothing silently makes the last
// capture the output and writes over it.
func Recorded(args []string) (Recording, error) {
	recording := Recording{
		Width: DefaultWidth, Height: DefaultHeight, Scheme: "dark", Frames: DefaultFrames,
	}
	if len(args) == 0 {
		return Recording{}, fmt.Errorf("usage: krewe record <file.webm> <capture> [<capture>...] " +
			"[<width>x<height>] [light|dark] [<frames per second>]\n\n" +
			"for example: krewe record run.webm frame-*.txt 900x400 2\n\n" +
			"capture the screen while the thing runs, once every half second, with " +
			"tmux capture-pane -t <session> -e -p > frame-01.txt")
	}
	recording.File = args[0]
	if !strings.EqualFold(filepath.Ext(recording.File), ".webm") {
		return Recording{}, fmt.Errorf("%q is not a recording: the encoder this system has writes webm "+
			"and nothing else, so name the file %s", recording.File, "run.webm")
	}
	for _, arg := range args[1:] {
		switch {
		case viewport.MatchString(arg):
			size := viewport.FindStringSubmatch(arg)
			recording.Width, _ = strconv.Atoi(size[1])
			recording.Height, _ = strconv.Atoi(size[2])
		case arg == "light" || arg == "dark":
			recording.Scheme = arg
		case aFrameRate(arg):
			recording.Frames, _ = strconv.Atoi(arg)
		default:
			recording.Captures = append(recording.Captures, arg)
		}
	}
	if len(recording.Captures) == 0 {
		return Recording{}, fmt.Errorf("krewe record joins captures of a screen into a recording and " +
			"this names none: capture the screen while the thing runs, once every half second, with " +
			"tmux capture-pane -t <session> -e -p > frame-01.txt, and name every frame here")
	}
	if len(recording.Captures) < 2 {
		return Recording{}, fmt.Errorf("%q is one frame, which is a picture rather than a recording: "+
			"capture the screen several times while the thing runs, or draw the one capture with krewe "+
			"render instead", recording.Captures[0])
	}
	file, err := filepath.Abs(recording.File)
	if err != nil {
		return Recording{}, fmt.Errorf("where to write %s: %w", recording.File, err)
	}
	recording.File = file
	return recording, nil
}

// aFrameRate says whether a word is a number of frames a second. A bare number is one, and it is the
// only bare number this command takes: a size carries its x and a scheme is a word.
func aFrameRate(arg string) bool {
	rate, err := strconv.Atoi(arg)
	return err == nil && rate > 0 && rate <= 60
}

// Record draws every capture and joins them, then says what it recorded.
//
// The frames go to a folder of their own that is removed afterwards, because a session that finds
// forty jpegs beside its work cannot tell them from what it built. What survives is the recording.
func Record(by Browser, with Recorder, recording Recording, out io.Writer) error {
	frames, err := os.MkdirTemp("", "krewe-frames-")
	if err != nil {
		return fmt.Errorf("nowhere to draw the frames: %w", err)
	}
	defer func() { _ = os.RemoveAll(frames) }()

	drawn := make([]string, 0, len(recording.Captures))
	for at, capture := range recording.Captures {
		where, err := TheCapture(capture)
		if err != nil {
			return err
		}
		frame := filepath.Join(frames, fmt.Sprintf("frame-%04d.jpg", at))
		if err := by.Draw(Drawing{
			URL: where, Shown: "the screen captured in " + capture, File: frame,
			Width: recording.Width, Height: recording.Height, Scheme: recording.Scheme,
			Wait: DefaultWait,
		}); err != nil {
			return err
		}
		drawn = append(drawn, frame)
	}
	if err := with.Join(drawn, recording); err != nil {
		return err
	}
	held, err := os.Stat(recording.File)
	if err != nil {
		return fmt.Errorf("the encoder reported no error and wrote nothing to %s: %w",
			recording.File, err)
	}
	// The label rule 45 asks a recording to carry: what it is of, how long it lasts and where it is.
	seconds := float64(len(recording.Captures)) / float64(recording.Frames)
	fmt.Fprintf(out, "recorded %d captures at %dx%d, %s, %.1f seconds, into %s (%d bytes)\n",
		len(recording.Captures), recording.Width, recording.Height, recording.Scheme, seconds,
		recording.File, held.Size())
	return nil
}

// Encoder is the program that joins frames, which is the one the headless browser brings with it.
type Encoder struct {
	// Name is the program. Empty means the one beside the browser.
	Name string
}

var _ Recorder = Encoder{}

// TheEncoder is the encoder on this machine, and the refusal where this machine has none.
//
// Three places, in the order a person would want them honoured: what they said, what is on the path,
// and what arrived with the browser. The refusal says the recording cannot be made here rather than
// naming a missing program, because what a session does next is attach one made elsewhere.
func TheEncoder(named string) (string, error) {
	if named != "" {
		if found, err := exec.LookPath(named); err == nil {
			return found, nil
		}
		return "", fmt.Errorf("%s names %q and there is no such program, so this system cannot record: "+
			"unset it, or record on your own machine and attach the file", FfmpegVariable, named)
	}
	if said := os.Getenv(FfmpegVariable); said != "" {
		return TheEncoder(said)
	}
	if found, err := exec.LookPath("ffmpeg"); err == nil {
		return found, nil
	}
	beside, _ := filepath.Glob(Encoders)
	for _, one := range beside {
		if held, err := os.Stat(one); err == nil && !held.IsDir() && held.Mode()&0o111 != 0 {
			return one, nil
		}
	}
	return "", fmt.Errorf("this system cannot record: there is no ffmpeg on the path and none beside "+
		"the headless browser. Record on your own machine and attach the file, or say what you would "+
		"have shown in steps a person can run. Set %s to name an encoder somewhere else", FfmpegVariable)
}

// Command is the whole invocation, in a function of its own so a test can read what would be run
// without an encoder to run it.
//
// The frames arrive on a pipe as jpeg, which is the one still image this build decodes, and the
// stream is named rather than probed: an encoder built with no image2 demuxer opens a numbered
// pattern as a file that is not there.
func (e Encoder) Command(frames []string, into Recording) *exec.Cmd {
	return exec.Command(e.Name,
		"-f", "image2pipe",
		"-c:v", "mjpeg",
		"-framerate", strconv.Itoa(into.Frames),
		"-i", "pipe:0",
		"-y", "-an",
		"-c:v", "vp8",
		"-b:v", "1M",
		"-deadline", "realtime",
		into.File,
	)
}

// Join runs the encoder over the drawn frames.
func (e Encoder) Join(frames []string, into Recording) error {
	found, err := TheEncoder(e.Name)
	if err != nil {
		return err
	}
	held := e
	held.Name = found
	command := held.Command(frames, into)
	piped, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("nothing to feed the encoder through: %w", err)
	}
	var said strings.Builder
	command.Stdout, command.Stderr = &said, &said
	if err := command.Start(); err != nil {
		return fmt.Errorf("starting the encoder: %w", err)
	}
	feeding := func() error {
		defer func() { _ = piped.Close() }()
		for _, frame := range frames {
			bytes, err := os.ReadFile(frame)
			if err != nil {
				return err
			}
			if _, err := piped.Write(bytes); err != nil {
				return err
			}
		}
		return nil
	}
	fed := feeding()
	if err := command.Wait(); err != nil {
		return fmt.Errorf("joining %d frames into %s: %w\n%s", len(frames), into.File, err,
			strings.TrimSpace(said.String()))
	}
	return fed
}
