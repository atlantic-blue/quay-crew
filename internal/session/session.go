// Package session holds what is true of a session itself, apart from where it is kept and how it is
// drawn.
//
// It exists because one of those facts was needed in two places at once. How long ago a session moved
// decides the order a listing comes back in, which the store answers, and it fills the last column of
// that listing, which the surfaces draw. Written down twice the two drift, and the listing then sorts
// on one clock and shows another, which is exactly the defect this package was cut for. Written down
// in either of the two callers, the other has to import it, and a store that reaches into a display
// package is a direction the next reader copies.
//
// So the rule lives with the thing it is about, the way a job's phases live in internal/job. Nothing
// here reads a database, draws a cell or knows a caller. It depends on the generated types and on
// nothing else, which is what lets both sides read it.
package session

import (
	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// LastMoved is when a session last went anywhere: when it was put away if it was, and when it was
// last touched otherwise.
//
// One rule over both, because a listing holds both and an operator reads one column. A live session
// has no archived stamp, so the fallback is the whole of the difference between the two cases.
//
// The archived stamp wins rather than the later of the two. A row that was put away still takes
// writes, so its touched stamp moves on afterwards, and measuring from that would say a session
// nobody can reach moved yesterday when it was filed a month ago.
//
// Nil for a session carrying neither stamp, which a caller renders as a dash and orders last. It is
// not an error: a caller holding a session that says nothing about itself should say nothing about it.
func LastMoved(one *quaycrewv1.Session) *timestamppb.Timestamp {
	if one.GetArchivedAt() != nil {
		return one.GetArchivedAt()
	}
	return one.GetUpdatedAt()
}
