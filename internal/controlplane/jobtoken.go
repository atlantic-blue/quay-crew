package controlplane

import (
	"context"
	"sync"
	"time"

	"github.com/atlantic-blue/krewe/internal/auth"
	"github.com/atlantic-blue/krewe/internal/store"
)

// jobCredentialLife is how long a credential minted for a job lasts when the job names no deadline.
//
// It is the backstop and not the control. What ends a credential is the job ending: the system takes
// the grant back the moment the job reaches a phase nothing moves it out of, so a session stops
// being able to call because its job is over rather than because a clock ran out.
//
// So this only has to cover a job whose end this process never sees, and it has to cover the whole
// of a job rather than a controller's hold on one. The two are different lengths and were the same
// constant: a hold is renewed on every tick, and a credential is handed to a sandbox once at
// dispatch and never refreshed, because refreshing it would mean re entering a running container.
// Tied to the hold, it lasted sixty seconds, and a root job that ran for twenty nine minutes
// declared none of its three children (issue 449).
//
// Twelve hours. The longest job on this system's record ran twenty nine minutes and the longest task
// seventeen, so nothing it has run comes near this, and a credential from a job the system lost track
// of does not survive into the next working day. A grant is held in this process besides, so a
// restart takes every one of them with it.
const jobCredentialLife = 12 * time.Hour

// forgetAfter is how long a credential that has stopped working is kept before the system drops it.
//
// Kept rather than dropped at once, because a credential the system cannot find is refused as a
// forgery, and that refusal sends a session looking for a fault in the token it was handed. A
// session that calls after its job ended is told the job ended, and this is how long the system can
// still say so.
const forgetAfter = time.Hour

// grants is every job credential this system has minted, by token.
//
// Kept in the process rather than in the store, and that is the one thing to know about it: a
// restart loses them. It costs nothing, because a restart also ends every task in flight, and a
// credential belongs to one task. What it buys is that a credential never outlives the process that
// handed it out.
type grants struct {
	mu   sync.RWMutex
	held map[string]minted
}

// minted is one credential and when it stopped working, which is zero while it still does.
type minted struct {
	grant auth.Grant
	// stoppedAt is when the system took this credential back, and zero for one it never took back. It
	// is what says when the credential may be forgotten, so a session calling after its job ended is
	// told that for a while rather than being told the token is a forgery.
	stoppedAt time.Time
}

// stopped is when this credential stopped working, and zero while it still does. A credential the
// system took back stopped when it was taken back, and one nobody took back stopped when it ran out.
func (m minted) stopped(now time.Time) time.Time {
	if !m.stoppedAt.IsZero() {
		return m.stoppedAt
	}
	if !m.grant.ExpiresAt.IsZero() && !m.grant.ExpiresAt.After(now) {
		return m.grant.ExpiresAt
	}
	return time.Time{}
}

func newGrants() *grants { return &grants{held: map[string]minted{}} }

// Grant reads back what a token holds. Whether it still works is the interceptor's to judge, so this
// answers with what was minted and nothing more.
func (g *grants) Grant(token string) (auth.Grant, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	held, known := g.held[token]
	return held.grant, known
}

// mint writes one credential and hands back the token that carries it.
func (g *grants) mint(grant auth.Grant) string {
	token := store.NewID() + store.NewID()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.forget(time.Now())
	g.held[token] = minted{grant: grant}
	return token
}

// revoke takes back every credential minted for a job, because the job ended in this phase.
//
// This is what ends a credential in a working system, and expiry is what is left for a job whose end
// the system never saw. It is marked rather than deleted so the refusal can name the job that ended.
func (g *grants) revoke(jobID, phase string) {
	if jobID == "" || phase == "" {
		return
	}
	now := time.Now().UTC()
	g.mu.Lock()
	defer g.mu.Unlock()
	for token, held := range g.held {
		if held.grant.Job != jobID || held.grant.Ended != "" {
			continue
		}
		held.grant.Ended, held.stoppedAt = phase, now
		g.held[token] = held
	}
	g.forget(now)
}

// forget drops the credentials that stopped working long enough ago that nobody is still asking
// about them. The caller holds the lock.
//
// On every mint and every revocation rather than on a timer: the set is small, a task mints one, and
// a map that only ever grows is a leak nobody sees until the process is old.
func (g *grants) forget(now time.Time) {
	for token, held := range g.held {
		if stopped := held.stopped(now); !stopped.IsZero() && now.Sub(stopped) > forgetAfter {
			delete(g.held, token)
		}
	}
}

// jobCredential mints the credential a task runs under, for the job it runs.
//
// It carries the verbs the job's role declared, and it is bound to the job identifier, which
// becomes the parent of anything that session declares. A session whose job runs as no role gets a
// credential that may call nothing: default deny, so a role is what grants and nothing else is.
//
// It lasts as long as the job may run: the job's own deadline where it names one, and the system's
// backstop where it does not. A session works for as long as its job does, and the system takes the
// credential back when the job ends.
func (s *Server) jobCredential(ctx context.Context, id string) (string, bool) {
	one, err := s.store.GetJob(ctx, id)
	if err != nil {
		return "", false
	}
	grant := auth.Grant{
		Job: one.ID, Workspace: one.Workspace, Project: one.Project,
		ExpiresAt: time.Now().UTC().Add(jobCredentialLife),
	}
	if one.Role != "" {
		if held, err := s.roleFor(ctx, one.Workspace, one.Role); err == nil {
			grant.Verbs = append([]string(nil), held.Verbs...)
		}
	}
	// A job that named a deadline said when it must be over, so its credential ends exactly there and
	// the system's own backstop does not apply: the backstop is a guess about a job that said nothing.
	if one.Deadline != nil {
		grant.ExpiresAt = one.Deadline.UTC()
	}
	return s.grants.mint(grant), true
}

// RevokeJobCredentials takes back every credential minted for a job, because the job ended in this
// phase. The controller calls it as it writes the end of a job, and so does an operator's stop.
//
// A session that calls afterwards is refused and told its job ended, which is a thing it can act on.
func (s *Server) RevokeJobCredentials(jobID, phase string) {
	s.grants.revoke(jobID, phase)
}

// Grants is what the interceptor asks to recognise a job credential.
func (s *Server) Grants() auth.Grants { return s.grants }

// credentialFor is the token a task runs under, empty for a task that runs no job.
func (s *Server) credentialFor(ctx context.Context, id string) string {
	if id == "" {
		return ""
	}
	token, minted := s.jobCredential(ctx, id)
	if !minted {
		return ""
	}
	return token
}

// JobCredentialForTest mints the credential a job would run under. Exported for a test,
// because what a credential carries is the whole boundary and asserting on it through a dispatch
// would assert on the sandbox instead.
func (s *Server) JobCredentialForTest(ctx context.Context, id string) (string, bool) {
	return s.jobCredential(ctx, id)
}
