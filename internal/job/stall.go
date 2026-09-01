package job

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
)

// The fifth comparison: is this system doing anything at all.
//
// The four above make reality match what was declared. This one asks whether they are working, and
// it is the only comparison here that is about the loop rather than about a row.
//
// Nothing running with something held is a state that is always wrong. It is not a slow system and
// it is not a busy machine: a busy machine has jobs running on it. It says the room is held by
// sandboxes doing nothing, and that nothing will change on its own, because the thing that would
// give the room back is the thing that has stopped.
//
// Measured on 31 August 2026. Twenty five jobs were declared, fifteen finished, and then five sat
// held saying a sandbox asks for 100 per cent and 0 per cent of 1200 per cent is unallocated. Twelve
// sandboxes were idle, every one of them for an hour or more, and between them they held the whole
// processor allocation. The workspace reclaim time was thirty minutes. Not one container came back,
// and an operator drained thirty three sessions by hand to free a resource the reclaim was already
// meant to free. See issue 575.
//
// **Why the pair, and not the pressure.** A full machine is healthy. Eight jobs running and
// seventeen waiting is exactly what admission is for, and taking a container back there takes it
// from a session that is about to get its next task. What is always wrong is the pair, so the pair
// is what is read.
//
// **Why here, and not at the declaration.** Section 5.1 decided that a job the machine cannot host
// stays pending for as long as it takes, and is never admitted and then killed. Refusing a
// declaration would turn away work the machine can do in ten minutes, and it would not have moved
// these five jobs, which were declared while the machine still had room.
//
// **Why this never stops a session doing work.** It takes back containers, and only containers that
// nothing is holding open. Stopping a sandbox because the machine is in danger is a different
// question with a different trigger, measured against what the runtime actually holds, and it is
// issue 478.
//
// **Why the two halves sit apart.** Acting on the state and describing it are two different
// moments. The description belongs to whoever writes the reason, which is hold, at the moment the
// job is turned away, so one writer owns that sentence and it is written once. The action belongs
// here.

// unstick takes one container back when this system has stopped moving.
//
// One container per tick, and the one idle longest. One is enough to start the queue again, and
// taking twelve would throw away eleven warm containers to answer a question that one answers.
func (c *Controller) unstick(ctx context.Context) {
	waiting, stopped := c.stoppedWith(ctx)
	if !stopped {
		return
	}
	if c.freedRoom(ctx, waiting) {
		return
	}
	// The state this system cannot leave on its own: nothing running, work waiting, and no container
	// it may take back. Every sandbox holding the machine has somebody attached to it, or the room is
	// held by something that is not a session this system knows about.
	c.logger.WarnContext(ctx,
		"this system is running nothing and could free no room for the work that is waiting",
		"held", len(waiting), "job", waiting[0].ID, "reason", waiting[0].Reason)
}

// stoppedWith reads the pair, and returns the jobs the machine turned away when it holds.
//
// The probe first, because it is the cheap half: one index lookup that costs the same on a system
// with a million finished jobs. The listing runs only when the probe says nothing is moving, so a
// working system pays the probe and nothing else.
func (c *Controller) stoppedWith(ctx context.Context) ([]*Job, bool) {
	moving, err := c.store.AnythingMoving(ctx)
	if err != nil {
		c.logger.WarnContext(ctx, "could not tell whether this system is doing anything", "error", err)
		return nil, false
	}
	if moving {
		return nil, false
	}
	waiting, err := c.store.TurnedAwayJob(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which jobs the machine turned away", "error", err)
		return nil, false
	}
	// Nothing running and nothing waiting is an idle system, which is the ordinary state of one.
	return waiting, len(waiting) > 0
}

// freedRoom takes back the container of the sandbox idle longest, and says whether it took one.
//
// The workspace's reclaim clock is not read here, and that is the point. That clock exists to save
// memory on a quiet system, and it is unset until three measurements are taken. This is a different
// question, it needs no measurement, and its answer is already known: the queue has stopped and a
// container nothing is using is holding it.
func (c *Controller) freedRoom(ctx context.Context, waiting []*Job) bool {
	sandboxes, err := c.store.IdleSandboxes(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which sessions still hold a container", "error", err)
		return false
	}
	for _, session := range sandboxes {
		// The same guard every reclaim has. A container an operator is typing into is never taken, and
		// a system that cannot tell reads that as attached.
		if c.attachedTo(ctx, session.GetId()) {
			continue
		}
		if _, err := c.plane.ReclaimSession(ctx,
			&quaycrewv1.ReclaimSessionRequest{Id: session.GetId()}); err != nil {
			// The session moved between the query and the write. The next one in the list is as good.
			c.logger.WarnContext(ctx, "could not take a session's container back to start the queue again",
				"session", session.GetId(), "error", err)
			continue
		}
		c.recordUnstuck(ctx, waiting[0], session, len(waiting))
		return true
	}
	return false
}

// recordUnstuck writes on the oldest waiting job that this system freed room for it.
//
// On that job, because it is the one being denied and the row an operator opens when they ask why
// nothing happened. The write goes through HoldJob carrying the reason the row already has, so the
// row does not move and the record lands in the same transaction as every other movement does.
// HoldJob applies only to a pending job, which is the condition wanted anyway: a job that started
// between the reclaim and this write needs no record of having waited.
func (c *Controller) recordUnstuck(ctx context.Context, waiting *Job,
	session *quaycrewv1.Session, held int) {
	detail := fmt.Sprintf("nothing was running and %s waiting for room, so the system took back %s, idle for %s",
		jobsWaiting(held), display.ShortID(session.GetHandle()), display.Age(session.GetUpdatedAt()))
	record := c.event(ctx, waiting, EventUnstuck, detail)
	if _, err := c.store.HoldJob(ctx, waiting.ID, waiting.Reason, record); err != nil {
		c.logger.WarnContext(ctx, "could not record that this system started its own queue again",
			"job", waiting.ID, "error", err)
	} else {
		c.exported(ctx, record)
	}
	c.logger.InfoContext(ctx, "this system had stopped, so it took a container back to start again",
		"session", session.GetId(), "job", waiting.ID, "held", held)
}

// whyItWaits is what a job the machine turned away says about itself.
//
// The room arithmetic, and then the part that changes what an operator does next. "There is not
// enough processor" reads the same on a busy machine and on a stopped one, and the two want
// different actions: on a busy machine, wait.
//
// Asked here rather than in unstick so that one writer owns this sentence. hold writes a reason only
// when it changes, so a job turned away by a stopped system carries the whole sentence from its
// first write and nothing rewrites it for as long as the state lasts.
func (c *Controller) whyItWaits(ctx context.Context, reason string, moving *stillMoving) string {
	if moving.yes(ctx, c) {
		return reason
	}
	return Stopped(reason)
}

// stillMoving is the answer to "is anything running" for one pass over the runnable jobs, asked at
// most once.
//
// It is born and dies inside a tick, and it is passed rather than kept, the way givenUp is: a
// controller keeps nothing in memory between ticks. What it buys is a burst of twenty held jobs
// costing one probe rather than twenty of the same one, and every one of them getting the same
// answer, which a job by job probe could not promise.
type stillMoving struct {
	asked bool
	value bool
}

// yes answers whether anything is running, and says yes where the system could not tell.
//
// The less alarming of the two on purpose: reporting a working system as stopped sends somebody to
// look at a machine that is fine.
func (m *stillMoving) yes(ctx context.Context, c *Controller) bool {
	if m.asked {
		return m.value
	}
	m.asked = true
	moving, err := c.store.AnythingMoving(ctx)
	if err != nil {
		c.logger.WarnContext(ctx, "could not tell whether this system is doing anything", "error", err)
		m.value = true
		return m.value
	}
	m.value = moving
	return m.value
}

// Stopped is a reason with the sentence about a system running nothing added to it.
func Stopped(reason string) string {
	switch {
	case reason == "":
		return stoppedSentence
	case strings.HasSuffix(reason, stoppedSentence):
		return reason
	default:
		return reason + ". " + stoppedSentence
	}
}

// stoppedSentence is that sentence, in one place, because the controller writes it and a reader of a
// job row is held to it.
const stoppedSentence = "Nothing else is running, so this system is stopped rather than busy"

// jobsWaiting keeps a record readable at one. "1 jobs were waiting" is a record written by nobody.
func jobsWaiting(count int) string {
	if count == 1 {
		return "1 job was"
	}
	return fmt.Sprintf("%d jobs were", count)
}
