package workspace

import (
	"context"
	"fmt"
	"sort"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
)

// Reading what the operator typed into a session, in one place.
//
// There were two of these, one for an address and one for a bare identifier, and they disagreed. One
// answered with the id and the other with the handle, one took both identifiers and the other took a
// handle only, and each wrote its own refusal. A session has two identifiers, so two readers of them
// is one too many: whichever a surface happened to call decided which identifier that surface would
// accept, and no two surfaces made the same choice.

// Session reads what the operator typed into the session it names.
//
// It takes either identifier, shortened the way a listing shortens it, and it takes an address. All
// of those reach the operator's screen, so all of them get typed back.
//
// The archived sessions are read second. A flow puts its own session away when it ends, and that
// session's history is the first thing anybody investigating the flow asks for, so which listing an
// identifier happens to sit in must not decide whether it can be named. Asked for second rather than
// merged in, so an identifier that names a live session today names the same one tomorrow.
func Session(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient,
	reference string) (*quaycrewv1.Session, error) {
	typed := strings.TrimSpace(reference)
	if typed == "" {
		return nil, fmt.Errorf("a session is required: the identifier the listing prints, or an address")
	}
	live, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		return nil, err
	}
	found, refused := sessionIn(ctx, client, typed, live.GetSessions())
	if refused == nil {
		return found, nil
	}
	putAway, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Archived: true})
	if err != nil {
		return nil, refused
	}
	if archived, missing := sessionIn(ctx, client, typed, putAway.GetSessions()); missing == nil {
		return archived, nil
	}
	return nil, refused
}

// SplitSession decides whether the first word names a session or begins the message.
//
// Two forms name a session. An address carries a separator, because it has to reach a project. A bare
// identifier is what the session column of a listing prints, so it is hexadecimal and at least as
// long as that column is wide.
//
// Anything else is the message, which keeps `quay dispatch hello there` a message rather than a
// mystifying lookup of "hello". A word that reads as an identifier and names no session is refused by
// Session, and never joined to the message: it used to become the first word of the text and start a
// new session, so the task went somewhere nobody asked for and nothing said so.
func SplitSession(args []string) (reference string, words []string) {
	if len(args) > 1 && NamesASession(args[0]) {
		return args[0], args[1:]
	}
	return "", args
}

// NamesASession says whether one word is meant as a session rather than as the start of a message.
func NamesASession(word string) bool {
	if strings.Contains(word, Separator) {
		return true
	}
	return display.LooksLikeIdentifier(word)
}

// sessionIn reads what the operator typed against one listing: an address, or one of the two
// identifiers a session has.
func sessionIn(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string,
	sessions []*quaycrewv1.Session) (*quaycrewv1.Session, error) {
	if strings.Contains(typed, Separator) {
		return sessionAtAddress(ctx, client, typed, sessions)
	}
	return sessionWithIdentifier(typed, sessions)
}

// sessionAtAddress reads an address, then finds the session it landed on in this listing.
func sessionAtAddress(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string,
	sessions []*quaycrewv1.Session) (*quaycrewv1.Session, error) {
	path, err := ParsePath(typed)
	if err != nil {
		return nil, err
	}
	if path.Session == "" {
		return nil, fmt.Errorf("%q names a project, not a session: add the session from the listing, for example %s/3cb04bf5",
			typed, typed)
	}
	located, err := ResolvePath(ctx, client, path)
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if session.GetHandle() == located.SessionID {
			return session, nil
		}
	}
	return nil, fmt.Errorf("%q resolved to a session the crew no longer lists", typed)
}

// sessionWithIdentifier matches a bare word against both identifiers a session has. An exact match
// wins outright, so a short identifier that happens to prefix another session still resolves to
// itself.
func sessionWithIdentifier(typed string, sessions []*quaycrewv1.Session) (*quaycrewv1.Session, error) {
	matches := make([]*quaycrewv1.Session, 0, 1)
	for _, session := range sessions {
		if session.GetId() == typed || session.GetHandle() == typed {
			return session, nil
		}
		if strings.HasPrefix(session.GetId(), typed) || strings.HasPrefix(session.GetHandle(), typed) {
			matches = append(matches, session)
		}
	}

	switch len(matches) {
	case 0:
		return nil, &NotFoundError{
			What: "session", Name: typed,
			Have: identifiersOf(sessions),
			Make: `start one with quay dispatch "..."`,
		}
	case 1:
		return matches[0], nil
	default:
		return nil, &AmbiguousError{What: "sessions", Name: typed, IDs: identifiersOf(matches)}
	}
}

// identifiersOf writes each session the way the session column of a listing writes it.
//
// The id alone. A refusal that also named the handle offered a value the operator's screen does not
// carry, which is the whole complaint the session column was raised against. The handle still reaches
// the session; it is simply not what a refusal offers.
func identifiersOf(sessions []*quaycrewv1.Session) []string {
	have := make([]string, 0, len(sessions))
	for _, session := range sessions {
		have = append(have, display.ShortID(session.GetId()))
	}
	sort.Strings(have)
	return have
}
