package console

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/capacity"
	"github.com/atlantic-blue/quay-krewe/internal/headroom"
)

// roomSummary is the room view's line above the columns: the word, what every container holds, the
// limit that binds, what is left, and how many more sandboxes that will hold.
//
// The view was one line per sandbox and nothing else. It answered which session to stop and never
// whether one had to be stopped at all: an operator could read eighteen rows of megabytes, add them
// up, and still not know how close the machine was. See issue 457.
//
// The figure is every container on the daemon rather than the rows above it, because the limit binds
// all of them and the system's own services are in there too. The line says so: the rows add up to
// less than this and an operator who is not told which figure they are reading distrusts both.
func roomSummary(answer *quaycrewv1.GetHeadroomResponse) (string, State) {
	if answer == nil {
		return "", StateUnknown
	}
	// Nothing measured this machine. Zeroes here would read as a machine holding nothing, so the line
	// says the word and the reason, which is what an operator goes and acts on.
	if answer.GetState() == headroom.StateUnknown || !measuredBoth(answer) {
		line := headroom.StateUnknown + "   the system has not read this machine"
		if why := answer.GetFailed(); why != "" {
			line += ": " + why
		}
		return line, StateStopped
	}

	free := freeBytes(answer)
	word, state := roomWordOf(answer.GetState(), free)
	return fmt.Sprintf("%s   containers hold %s of %s   %s left, %s",
		word, answer.GetUsed(), answer.GetLimit(), capacity.Memory(free), fitPhrase(free)), state
}

// measuredBoth says whether the two figures the arithmetic needs were read. A byte count is negative
// where nothing measured it, and zero is a real reading of a machine holding nothing.
func measuredBoth(answer *quaycrewv1.GetHeadroomResponse) bool {
	return answer.GetUsedBytes() >= 0 && answer.GetLimitBytes() >= 0
}

// freeBytes is what the daemon may still hand out. A daemon holding more than its own cap has
// nothing left rather than a negative amount left: the two figures come from two commands, and a
// container started between them is real.
func freeBytes(answer *quaycrewv1.GetHeadroomResponse) int64 {
	if free := answer.GetLimitBytes() - answer.GetUsedBytes(); free > 0 {
		return free
	}
	return 0
}

// fitPhrase is what is left, in the unit the operator acts in: sandboxes.
//
// A sandbox asks for a measured 1,536 mebibytes, read every two seconds over 808 samples of the work
// this system's sandboxes do. See internal/capacity/measured.go. Dividing megabytes by that in your
// head is exactly the arithmetic this line exists to save.
func fitPhrase(free int64) string {
	switch fits := free / capacity.RequestMemory; fits {
	case 0:
		return "not enough for another sandbox"
	case 1:
		return "enough for one more sandbox"
	default:
		return fmt.Sprintf("enough for %d more sandboxes", fits)
	}
}

// roomWordOf is the word the line carries and the colour it is drawn in.
//
// The system's own word comes first, so the view and the header never say two different things about
// one machine. It is a fraction of the binding limit and the repository says plainly that nothing
// measured those fractions, so one case is added underneath: a margin that will not hold another
// sandbox is never healthy, however small a fraction of the machine it is. That threshold is
// measured, and on a small daemon cap it turns where three quarters has not been reached.
//
// The two nest rather than disagree. Tight begins at a quarter of the limit free, which on the 7,837
// mebibyte cap in the incident is 1,959 mebibytes, and a sandbox asks for 1,536: on that machine the
// fraction turns first and this changes nothing.
func roomWordOf(state string, free int64) (string, State) {
	if state == headroom.StateRoom && free < capacity.RequestMemory {
		return headroom.StateTight, StateBusy
	}
	switch state {
	case headroom.StateFull:
		// Uppercase, the way the header writes it: full has to be readable without reading the
		// number beside it, and on a console piped to a file there is no colour to read either.
		return strings.ToUpper(state), StateFailed
	case headroom.StateTight:
		return state, StateBusy
	default:
		return state, StateReady
	}
}

// roomSummaryFrom asks the system what the machine has left and writes the view's line from it.
//
// It is a second call beside the listing's own, and it costs nothing: the system answers both from the
// sample it took on its own timer, which is the whole reason the header may ask every second.
func roomSummaryFrom(client quaycrewv1.ControlPlaneServiceClient) Summariser {
	return func(ctx context.Context, _ string) (string, State) {
		answer, err := client.GetHeadroom(ctx, &quaycrewv1.GetHeadroomRequest{})
		if err != nil {
			// The listing made the same call and reports the same failure, so saying it twice would
			// put an error above the rows and another under them.
			return "", StateUnknown
		}
		return roomSummary(answer)
	}
}
