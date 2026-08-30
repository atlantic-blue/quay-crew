package session_test

import (
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/session"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func stamp(hoursAgo int) *timestamppb.Timestamp {
	return timestamppb.New(time.Now().UTC().Add(-time.Duration(hoursAgo) * time.Hour))
}

// A live session carries no archived stamp, so it is measured from when it was last touched.
func TestALiveSessionMovedWhenItWasLastTouched(t *testing.T) {
	touched := stamp(3)
	one := &quaycrewv1.Session{CreatedAt: stamp(24 * 7), UpdatedAt: touched}

	if got := session.LastMoved(one); !got.AsTime().Equal(touched.AsTime()) {
		t.Fatalf("a live session moved at %s, want the touched stamp %s", got.AsTime(), touched.AsTime())
	}
}

// An archived session is measured from when it was put away, and its row keeps taking writes after
// that. Measuring from the touched stamp would say a session nobody can reach moved an hour ago.
func TestAnArchivedSessionMovedWhenItWasPutAway(t *testing.T) {
	putAway := stamp(24 * 7)
	one := &quaycrewv1.Session{CreatedAt: stamp(24 * 9), UpdatedAt: stamp(1), ArchivedAt: putAway}

	if got := session.LastMoved(one); !got.AsTime().Equal(putAway.AsTime()) {
		t.Fatalf("an archived session moved at %s, want the archived stamp %s", got.AsTime(), putAway.AsTime())
	}
}

// The archived stamp wins rather than the later of the two, so a row written to a moment ago still
// reads from when it was filed.
func TestTheArchivedStampWinsEvenWhenItIsTheOlderOne(t *testing.T) {
	putAway := stamp(24 * 30)
	one := &quaycrewv1.Session{UpdatedAt: stamp(0), ArchivedAt: putAway}

	if got := session.LastMoved(one); !got.AsTime().Equal(putAway.AsTime()) {
		t.Fatalf("the later stamp won: got %s, want the archived stamp %s", got.AsTime(), putAway.AsTime())
	}
}

// Nothing known about a session is not an error. A caller renders it as a dash and orders it last, so
// what it needs back is an absence rather than a moment in 1970 dressed up as one.
func TestASessionWithNoStampsHasNotMoved(t *testing.T) {
	if got := session.LastMoved(&quaycrewv1.Session{}); got != nil {
		t.Fatalf("a session with no stamps moved at %v, want nothing", got)
	}
}

// A nil session reaches this from a listing that came back short, and it must answer rather than
// take the caller down with it.
func TestANilSessionHasNotMoved(t *testing.T) {
	if got := session.LastMoved(nil); got != nil {
		t.Fatalf("a session that is not there moved at %v, want nothing", got)
	}
}
