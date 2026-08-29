package controlplane

import (
	"context"
	"sync"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// jobTokenLife is how long a credential minted for a job lasts when the job names no
// deadline. It is the length of the crew's own lease, because a credential outliving the hold on the
// job it belongs to would be a grant nobody is watching.
const jobTokenLife = job.DefaultLease

// grants is every job credential this crew has minted, by token.
//
// Kept in the process rather than in the store, and that is the one thing to know about it: a
// restart loses them. It costs nothing, because a restart also ends every task in flight, and a
// credential belongs to one task. What it buys is that a credential never outlives the process that
// handed it out.
type grants struct {
	mu   sync.RWMutex
	held map[string]auth.Grant
}

func newGrants() *grants { return &grants{held: map[string]auth.Grant{}} }

// Grant reads back what a token holds. Expiry is the interceptor's to check, so this answers with
// what was minted and nothing more.
func (g *grants) Grant(token string) (auth.Grant, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	held, minted := g.held[token]
	return held, minted
}

// mint writes one credential and hands back the token that carries it.
func (g *grants) mint(grant auth.Grant) string {
	token := store.NewID() + store.NewID()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.forget(time.Now())
	g.held[token] = grant
	return token
}

// forget drops the credentials that have run out. The caller holds the lock.
//
// On every mint rather than on a timer: the set is small, a task mints one, and a map that only ever
// grows is a leak nobody sees until the process is old.
func (g *grants) forget(now time.Time) {
	for token, held := range g.held {
		if !held.ExpiresAt.IsZero() && !held.ExpiresAt.After(now) {
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
// It expires no later than the job's deadline, so a credential that leaks out of a sandbox stops
// working when the job it belongs to would have.
func (s *Server) jobCredential(ctx context.Context, id string) (string, bool) {
	one, err := s.store.GetJob(ctx, id)
	if err != nil {
		return "", false
	}
	grant := auth.Grant{
		Job: one.ID, Workspace: one.Workspace, Project: one.Project,
		ExpiresAt: time.Now().UTC().Add(jobTokenLife),
	}
	if one.Role != "" {
		if held, err := s.roleFor(ctx, one.Workspace, one.Role); err == nil {
			grant.Verbs = append([]string(nil), held.May_...)
		}
	}
	if one.Deadline != nil && one.Deadline.Before(grant.ExpiresAt) {
		grant.ExpiresAt = *one.Deadline
	}
	return s.grants.mint(grant), true
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
