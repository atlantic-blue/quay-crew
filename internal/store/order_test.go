package store

import (
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The rule under test is the one a listing's last column reads: a session is measured from when it
// was put away if it was, and from when it was last touched otherwise. The order used to be the
// created stamp instead, which is why these cases all set the two stamps apart on purpose.

// at is a stamp a whole number of hours ago, so a case reads as the ages an operator would see.
func at(hoursAgo int) *timestamppb.Timestamp {
	return timestamppb.New(time.Now().UTC().Add(-time.Duration(hoursAgo) * time.Hour))
}

func aSession(id string, created, updated, archived *timestamppb.Timestamp) *quaycrewv1.Session {
	return &quaycrewv1.Session{Id: id, CreatedAt: created, UpdatedAt: updated, ArchivedAt: archived}
}

func order(sessions []*quaycrewv1.Session) []string {
	out := make([]string, 0, len(sessions))
	for _, one := range sessions {
		out = append(out, one.GetId())
	}
	return out
}

func same(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the listing is %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("the listing is %v, want %v", got, want)
		}
	}
}

// The defect itself: a session made a week ago and used an hour ago belongs above one made
// yesterday and untouched since. Sorted on the created stamp it is the other way round, and the age
// column then runs 1d, 7d rather than 1h, 1d.
func TestASessionMadeEarlierAndTouchedLaterComesFirst(t *testing.T) {
	old := aSession("old", at(24*7), at(1), nil)
	fresh := aSession("fresh", at(24), at(24), nil)

	list := []*quaycrewv1.Session{fresh, old}
	sortByLastMoved(list)

	same(t, order(list), []string{"old", "fresh"})
}

// The other stamp is the point of the rule rather than a special case: an archived session is
// measured from when it was put away, so writing to its row afterwards must not lift it back up the
// list. Ordered on the touched stamp alone this comes back the other way round.
func TestAnArchivedSessionIsOrderedByWhenItWasPutAway(t *testing.T) {
	// The created stamps run against the answer as well, so this case is red for an order taken from
	// either of the two stamps it is not about.
	early := aSession("early", at(24*8), at(1), at(24*7))
	late := aSession("late", at(24*9), at(24*2), at(24*2))

	list := []*quaycrewv1.Session{early, late}
	sortByLastMoved(list)

	// late was put away two days ago and early a week ago, whatever has been written to either row
	// since.
	same(t, order(list), []string{"late", "early"})
}

// A live session and an archived one read from different fields, and one listing holds both while a
// session is being put away. One rule covers them, so the two compare against each other.
func TestALiveSessionAndAnArchivedOneShareOneOrder(t *testing.T) {
	live := aSession("live", at(24*30), at(24*3), nil)
	archived := aSession("archived", at(24*30), at(1), at(24*5))

	list := []*quaycrewv1.Session{archived, live}
	sortByLastMoved(list)

	// The live one moved three days ago and the archived one five, even though the archived row was
	// written to an hour ago.
	same(t, order(list), []string{"live", "archived"})
}

// Two sessions can share a moment, and an order that then depends on which one the store happened to
// hand over is an order that changes under the operator between two refreshes.
func TestSessionsSharingAMomentAreOrderedByIdentifier(t *testing.T) {
	moment := at(3)
	first := aSession("aaa", at(24), moment, nil)
	second := aSession("bbb", at(48), moment, nil)

	// Both ways round, because a tiebreaker that is really the input order passes one of these.
	for _, list := range [][]*quaycrewv1.Session{{first, second}, {second, first}} {
		sortByLastMoved(list)
		same(t, order(list), []string{"aaa", "bbb"})
	}
}

// A row with no stamps at all must not take the listing down with it. The age column already shows a
// dash for one, and the order puts it last, where a session nothing is known about belongs.
func TestASessionWithNoStampsSortsLastRatherThanPanicking(t *testing.T) {
	blank := aSession("blank", nil, nil, nil)
	touched := aSession("touched", at(24*365), at(24*365), nil)

	list := []*quaycrewv1.Session{blank, touched}
	sortByLastMoved(list)

	same(t, order(list), []string{"touched", "blank"})
}
