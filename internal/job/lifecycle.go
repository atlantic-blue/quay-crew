package job

import (
	"context"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
)

// The fourth query, and what the controller does with it: putting sessions away.
//
// The system never put a session away on its own before this. Nothing in the code removed a container
// without somebody asking, so a session that answered one question in March still held its container
// in August unless the system had restarted.
//
// A session is not a second resource with a declaration of its own. What is wanted of it is derived
// from the job that names it, which is why this belongs to the same loop rather than to a second
// one, and why the controller keeps nothing in memory between ticks.
//
// The rule, in three lines:
//
//   - A session named by job in a non terminal phase is wanted alive, and nothing here touches it.
//     The store's queries are what leave it out.
//   - A settled session idle for longer than its workspace's reclaim time is wanted reclaimed: the
//     container goes and everything else stays.
//   - A session reclaimed for longer than its workspace's archive time is wanted archived.
//
// **Both times ship unset, and unset means this does nothing at all.** That is deliberate and it is
// not a placeholder. Three measurements decide the numbers and none has been taken: the distribution
// of the gap between one task landing in a session and the next starting, what a resume costs, and
// what an idle container holds. Section 11 of docs/ORCHESTRATION.md names each and the command that
// would take it. Until those runs exist the system refuses a number it was never given rather than
// choosing one, because a reclaim time set below the real idle gap throws away containers that were
// about to be used.
//
// **The two rules are two queries, and that is the fault this closes.** They used to be one: every
// settled session, ordered by how long ago it was touched, capped at a batch. A reclaimed session is
// settled and stays settled, because with no archive time nothing ever moves it, and its stamp is
// older than that of a sandbox idle for an hour. So the batch filled with rows nothing could act on
// and the reclaim stopped reaching a container at all, permanently. Twelve sandboxes then held a
// whole machine's processors while five jobs waited for room. See issue 575.

// putAway takes back the containers of sessions nothing is holding open, and files the ones that have
// been back for long enough.
//
// Two queries per tick, each on an index the store already has, and one movement per session per
// tick: a session reclaimed on this tick is archived on a later one, never both at once, so every
// step is on the record separately. A session cannot be in both queries, which is what keeps that
// property true now that there are two.
func (c *Controller) putAway(ctx context.Context) {
	now := time.Now().UTC()
	sandboxes, err := c.store.IdleSandboxes(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which sessions still hold a container", "error", err)
	}
	for _, session := range sandboxes {
		c.reclaimIdle(ctx, session, c.limitsFor(ctx, session).Reclaim(), now)
	}
	reclaimed, err := c.store.ReclaimedSessions(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which sessions have already given their container back",
			"error", err)
		return
	}
	for _, session := range reclaimed {
		c.archiveAged(ctx, session, c.limitsFor(ctx, session).Archive(), now)
	}
}

// limitsFor is what this session's workspace allows, and nothing at all where it could not be read. A
// workspace whose row will not come back leaves its sessions alone rather than being reclaimed
// against a number nobody set.
func (c *Controller) limitsFor(ctx context.Context, session *quaycrewv1.Session) Limits {
	limits, err := c.store.WorkspaceLimits(ctx, session.GetWorkspace())
	if err != nil {
		c.logger.WarnContext(ctx, "could not read a workspace's ceiling, so its sessions are left alone",
			"session", session.GetId(), "workspace", session.GetWorkspace(), "error", err)
		return Limits{Workspace: session.GetWorkspace()}
	}
	return limits
}

// reclaimIdle takes one session's container back, if it has been idle for longer than its workspace
// allows and nobody is typing into it.
func (c *Controller) reclaimIdle(ctx context.Context, session *quaycrewv1.Session,
	reclaim time.Duration, now time.Time) {
	if reclaim <= 0 {
		return
	}
	if idle := now.Sub(session.GetUpdatedAt().AsTime()); idle < reclaim {
		return
	}
	// Asked last, so the exec this costs is spent only on a session that is otherwise about to be
	// reclaimed. A system whose reclaim time is unset never reaches this line at all, which is why the
	// unmeasured cost of the signal is not a reason to hold the mechanism back.
	if c.attachedTo(ctx, session.GetId()) {
		return
	}
	if _, err := c.plane.ReclaimSession(ctx, &quaycrewv1.ReclaimSessionRequest{Id: session.GetId()}); err != nil {
		// The session moved between the query and the write: a task arrived, or somebody stopped it.
		// The next tick reads the world again, so one row that would not move is not worth more here.
		c.logger.WarnContext(ctx, "could not take a session's container back",
			"session", session.GetId(), "error", err)
	}
}

// archiveAged files one reclaimed session away, if it has been reclaimed for longer than its
// workspace allows.
//
// Nobody is asked about attachment here, because a reclaimed session has no container for anybody to
// be attached to. Opening one restores and restarts it, which takes it out of this state before this
// runs again.
func (c *Controller) archiveAged(ctx context.Context, session *quaycrewv1.Session,
	archive time.Duration, now time.Time) {
	if archive <= 0 {
		return
	}
	at := session.GetReclaimedAt()
	if at == nil {
		// Reclaimed with no stamp is a row this system did not write, so there is nothing to measure
		// against and nothing is done. Guessing at updated_at here would file it away on the first
		// tick after an upgrade.
		return
	}
	if since := now.Sub(at.AsTime()); since < archive {
		return
	}
	if _, err := c.plane.ArchiveSession(ctx, &quaycrewv1.ArchiveSessionRequest{Id: session.GetId()}); err != nil {
		c.logger.WarnContext(ctx, "could not file a reclaimed session away",
			"session", session.GetId(), "error", err)
	}
}

// attachedTo says whether somebody has this session's conversation open, and answers yes whenever the
// system cannot tell.
//
// A controller with no way to ask must never reclaim, which is why nil answers attached rather than
// being treated as no. Wiring the signal is what turns the mechanism on, not a flag.
func (c *Controller) attachedTo(ctx context.Context, session string) bool {
	if c.attached == nil {
		return true
	}
	open, err := c.attached.SessionAttached(ctx, session)
	if err != nil {
		c.logger.WarnContext(ctx, "could not tell whether anybody is in a session, so it is left alone",
			"session", session, "error", err)
		return true
	}
	return open
}

// StatusReclaimed is the session status the system writes when it takes a container back. It is the
// control plane's word, kept here so the controller reads a session without depending on the package
// that writes one, the way the task statuses above already are.
const StatusReclaimed = "reclaimed"
