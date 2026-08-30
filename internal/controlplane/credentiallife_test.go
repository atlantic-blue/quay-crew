package controlplane_test

import (
	"context"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// How long a session's credential lasts, and what ends it.
//
// The credential is handed to a sandbox once at dispatch and nothing refreshes it, because
// refreshing it would mean re entering a running container. It was minted for the length of the
// controller's hold on the job, which is a different lifetime: a hold is renewed on every tick, and
// sixty seconds of it left a root job that ran twenty nine minutes unable to declare any of its
// three children (issue 449).

// The load bearing one. A credential has to cover the job it was minted for, so it has to outlast
// the controller's hold by a long way.
func TestACredentialOutlastsTheControllersHoldOnItsJobByALongWay(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	declared := declareJob(t, s, project, "read the electricity bill")

	token, minted := s.JobCredentialForTest(context.Background(), declared.GetId())
	if !minted {
		t.Fatal("no credential was minted for a job")
	}
	grant, recognised := s.Grants().Grant(token)
	if !recognised {
		t.Fatal("the system does not recognise the credential it minted")
	}

	lasts := time.Until(grant.ExpiresAt)
	// The run that found this ran for twenty nine minutes. An hour is the smallest number that is
	// clearly about a job rather than about a tick, and it is thirty of these leases.
	if lasts < time.Hour {
		t.Fatalf("the credential lasts %s, and a session works for hours: a job's session could not "+
			"declare a child after %s of it", lasts.Round(time.Second), job.DefaultLease)
	}
	if lasts <= job.DefaultLease {
		t.Fatalf("the credential lasts %s, which is the controller's hold on the job. The hold is "+
			"renewed on every tick and the credential is never refreshed, so they are not one length",
			lasts.Round(time.Second))
	}
}

// A job that named a deadline said when it must be over, so its credential ends there. Both ways
// round: a deadline is the whole answer and not only a ceiling on the system's own guess.
func TestACredentialEndsWithTheJobsOwnDeadline(t *testing.T) {
	for _, one := range []struct {
		name string
		in   time.Duration
	}{
		{name: "a deadline sooner than the system's backstop", in: 90 * time.Second},
		{name: "a deadline further out than the system's backstop", in: 72 * time.Hour},
	} {
		t.Run(one.name, func(t *testing.T) {
			s := newServer(&model.FakeRunner{})
			_, project := newProject(t, s)
			deadline := time.Now().UTC().Add(one.in).Truncate(time.Second)
			declared, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
				Project: project, Title: "read the electricity bill", Brief: "open it",
				Deadline: timestamppb.New(deadline),
			})
			if err != nil {
				t.Fatalf("CreateJob: %v", err)
			}

			token, _ := s.JobCredentialForTest(context.Background(), declared.GetJob().GetId())
			grant, _ := s.Grants().Grant(token)

			if !grant.ExpiresAt.Equal(deadline) {
				t.Fatalf("the credential runs to %v, want the job's own deadline of %v",
					grant.ExpiresAt, deadline)
			}
		})
	}
}

// What ends a credential in a working system is the job ending, and expiry is only the backstop
// behind that. A job an operator stopped is over, so its session stops being able to call.
func TestTheSystemTakesACredentialBackWhenTheOperatorStopsTheJob(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	declared := declareJob(t, s, project, "read the electricity bill")
	ctx := context.Background()

	token, _ := s.JobCredentialForTest(ctx, declared.GetId())
	if grant, _ := s.Grants().Grant(token); grant.Ended != "" {
		t.Fatalf("the credential reads as ended (%q) while its job is running", grant.Ended)
	}

	if _, err := s.StopJob(ctx, &quaycrewv1.StopJobRequest{
		Id: declared.GetId(), Reason: "I have had enough",
	}); err != nil {
		t.Fatalf("StopJob: %v", err)
	}

	// Still known, and known to be over. A credential the system cannot find at all is refused as a
	// forgery, and that refusal sends a session looking for a fault in the token it was handed.
	grant, recognised := s.Grants().Grant(token)
	if !recognised {
		t.Fatal("the system has forgotten the credential, so a session that calls is told its token is a forgery")
	}
	if grant.Ended != job.PhaseStopped {
		t.Fatalf("the credential reads as %q, want the phase the job ended in, %q", grant.Ended, job.PhaseStopped)
	}
	if grant.ExpiresAt.Before(time.Now()) {
		t.Fatal("this proves nothing: the credential had run out anyway, so the stop is not what ended it")
	}
}

// One job's end takes back one job's credentials. A system that took every credential back would stop
// every session in it.
func TestStoppingOneJobLeavesAnotherJobsCredentialAlone(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	ctx := context.Background()
	stopped := declareJob(t, s, project, "read the electricity bill")
	other := declareJob(t, s, project, "read the water bill")

	stoppedToken, _ := s.JobCredentialForTest(ctx, stopped.GetId())
	otherToken, _ := s.JobCredentialForTest(ctx, other.GetId())

	if _, err := s.StopJob(ctx, &quaycrewv1.StopJobRequest{Id: stopped.GetId()}); err != nil {
		t.Fatalf("StopJob: %v", err)
	}

	if grant, _ := s.Grants().Grant(stoppedToken); grant.Ended == "" {
		t.Fatal("the credential of the job that was stopped still works")
	}
	if grant, _ := s.Grants().Grant(otherToken); grant.Ended != "" {
		t.Fatalf("the credential of a job nobody stopped reads as %q", grant.Ended)
	}
}
