package forge_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/forge"
)

// The reader, held against the shapes GitHub actually answers with.
//
// The query these bodies answer was run against the real service on 31 August 2026, against
// atlantic-blue/quay-crew pull request 572, and the bodies below are that answer's shape. A double
// looser than the service it stands for is how a suite stays green while the real call fails.

const anAddress = "https://github.com/atlantic-blue/quay-crew/pull/549"

func at(t *testing.T, address string) forge.Address {
	t.Helper()
	parsed, err := forge.Parse(address)
	if err != nil {
		t.Fatalf("Parse(%q): %v", address, err)
	}
	return parsed
}

// The refusals first, because a gate that always passes satisfies every test about passing.

func TestASystemWithNoTokenIsToldTheCommandThatSetsOne(t *testing.T) {
	reading, err := (&forge.GitHub{}).Read(context.Background(), at(t, anAddress))
	if err == nil {
		t.Fatalf("a system with no credential read a pull request anyway: %v", reading)
	}
	if !strings.Contains(err.Error(), "krewe secret set system GH_TOKEN") {
		t.Fatalf("the refusal is %q, and it never says how to set one", err)
	}
}

func TestATokenTheSystemCannotReadIsTheSameRefusal(t *testing.T) {
	for _, one := range []struct {
		name  string
		token func(context.Context) (string, error)
	}{
		{"the secret store would not answer", func(context.Context) (string, error) {
			return "", fmt.Errorf("secrets: not found")
		}},
		{"the secret is set and empty", func(context.Context) (string, error) { return "   ", nil }},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := (&forge.GitHub{Token: one.token}).Read(context.Background(), at(t, anAddress))
			if err == nil {
				t.Fatal("it read a pull request with no credential")
			}
			if !strings.Contains(err.Error(), forge.TokenName) {
				t.Fatalf("the refusal is %q, and it never names the secret", err)
			}
		})
	}
}

// This build ships one forge. A pull request anywhere else is refused by name rather than read with a
// client that has never been run against that service.
func TestAPullRequestOnAnotherForgeIsRefusedByName(t *testing.T) {
	reader := &forge.GitHub{Token: aToken}
	_, err := reader.Read(context.Background(), at(t, "https://git.example.com/acme/bills/pull/3"))
	if err == nil {
		t.Fatal("a pull request on another forge was read")
	}
	if !strings.Contains(err.Error(), "git.example.com") {
		t.Fatalf("the refusal is %q, and it never names the host", err)
	}
}

func TestAForgeThatRefusesTheCallIsAFailedReading(t *testing.T) {
	for _, one := range []struct {
		name   string
		status int
		limit  string
		says   string
	}{
		{name: "a credential the forge does not accept", status: http.StatusUnauthorized, limit: "4999", says: "401"},
		{name: "the rate limit is spent", status: http.StatusForbidden, limit: "0", says: "rate limit"},
		{name: "the forge is having a bad day", status: http.StatusBadGateway, limit: "4999", says: "502"},
	} {
		t.Run(one.name, func(t *testing.T) {
			forgeSaid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", one.limit)
				w.WriteHeader(one.status)
				fmt.Fprint(w, `{}`)
			}))
			defer forgeSaid.Close()

			_, err := readerAgainst(forgeSaid.URL).Read(context.Background(), at(t, anAddress))
			if err == nil {
				t.Fatal("a refused call came back as a reading")
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, and it never says %q", err, one.says)
			}
		})
	}
}

// GraphQL answers 200 and puts the refusal in the body, so a reader switching on the status code
// alone would read a refusal as an empty pull request, which is the shape that reads as green.
func TestARefusalInTheBodyIsNotAReading(t *testing.T) {
	for _, one := range []struct {
		name string
		body string
		says string
	}{
		{
			name: "the forge names what it will not answer",
			body: `{"errors":[{"message":"Could not resolve to a Repository with the name 'atlantic-blue/quay-crew'."}]}`,
			says: "Could not resolve",
		},
		{
			name: "the credential cannot see the repository",
			body: `{"data":{"repository":null}}`,
			says: "cannot see it",
		},
		{
			name: "there is no pull request with that number",
			body: `{"data":{"repository":{"pullRequest":null}}}`,
			says: "no pull request",
		},
		{
			name: "the answer is not the shape this build reads",
			body: `not json at all`,
			says: "cannot read",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			forgeSaid := answering(one.body)
			defer forgeSaid.Close()
			reading, err := readerAgainst(forgeSaid.URL).Read(context.Background(), at(t, anAddress))
			if err == nil {
				t.Fatalf("a refusal came back as the reading %v", reading)
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, and it never says %q", err, one.says)
			}
		})
	}
}

// And then the answers. Each one is a whole body, so what is asserted is the reading this build makes
// of what GitHub sends rather than of a shape this test invented.
func TestWhatTheForgeSaysBecomesAReading(t *testing.T) {
	for _, one := range []struct {
		name        string
		body        string
		status      string
		checks      string
		failedCheck string
		review      string
	}{
		{
			name:   "open, and every check passed",
			body:   pullRequest(`"OPEN"`, false, `null`, rollup("SUCCESS", checkRun("unit", "COMPLETED", "SUCCESS"))),
			status: forge.StatusOpen, checks: forge.ChecksGreen, review: forge.ReviewNone,
		},
		{
			name:   "merged",
			body:   pullRequest(`"MERGED"`, true, `null`, rollup("SUCCESS", checkRun("unit", "COMPLETED", "SUCCESS"))),
			status: forge.StatusMerged, checks: forge.ChecksGreen, review: forge.ReviewNone,
		},
		{
			name:   "closed without merging",
			body:   pullRequest(`"CLOSED"`, false, `null`, rollup("SUCCESS", checkRun("unit", "COMPLETED", "SUCCESS"))),
			status: forge.StatusClosed, checks: forge.ChecksGreen, review: forge.ReviewNone,
		},
		{
			name: "a check failed, and the failure is named",
			body: pullRequest(`"OPEN"`, false, `null`, rollup("FAILURE",
				checkRun("unit", "COMPLETED", "SUCCESS"), checkRun("integration", "COMPLETED", "FAILURE"))),
			status: forge.StatusOpen, checks: forge.ChecksRed, failedCheck: "integration", review: forge.ReviewNone,
		},
		{
			name: "one failure and nine still running is red, not pending",
			body: pullRequest(`"OPEN"`, false, `null`, rollup("PENDING",
				checkRun("lint", "COMPLETED", "FAILURE"), checkRun("unit", "IN_PROGRESS", ""))),
			status: forge.StatusOpen, checks: forge.ChecksRed, failedCheck: "lint", review: forge.ReviewNone,
		},
		{
			name: "a run that has not finished is pending whatever its conclusion field holds",
			body: pullRequest(`"OPEN"`, false, `null`, rollup("PENDING",
				checkRun("unit", "IN_PROGRESS", ""), checkRun("lint", "COMPLETED", "SUCCESS"))),
			status: forge.StatusOpen, checks: forge.ChecksPending, review: forge.ReviewNone,
		},
		{
			name: "a commit status that failed is red too, named by its context",
			body: pullRequest(`"OPEN"`, false, `null`, rollup("FAILURE",
				checkRun("unit", "COMPLETED", "SUCCESS"), statusContext("coverage/project", "FAILURE"))),
			status: forge.StatusOpen, checks: forge.ChecksRed, failedCheck: "coverage/project", review: forge.ReviewNone,
		},
		{
			name:   "a review asked for changes",
			body:   pullRequest(`"OPEN"`, false, `"CHANGES_REQUESTED"`, rollup("SUCCESS", checkRun("unit", "COMPLETED", "SUCCESS"))),
			status: forge.StatusOpen, checks: forge.ChecksGreen, review: forge.ReviewChangesRequested,
		},
		{
			name:   "a review approved it",
			body:   pullRequest(`"OPEN"`, false, `"APPROVED"`, rollup("SUCCESS", checkRun("unit", "COMPLETED", "SUCCESS"))),
			status: forge.StatusOpen, checks: forge.ChecksGreen, review: forge.ReviewApproved,
		},
		{
			name:   "a pull request nothing has run a check on",
			body:   pullRequest(`"OPEN"`, false, `null`, `null`),
			status: forge.StatusOpen, checks: forge.ChecksNone, review: forge.ReviewNone,
		},
		{
			name:   "a cancelled run is not a passing one",
			body:   pullRequest(`"OPEN"`, false, `null`, rollup("FAILURE", checkRun("containers", "COMPLETED", "CANCELLED"))),
			status: forge.StatusOpen, checks: forge.ChecksRed, failedCheck: "containers", review: forge.ReviewNone,
		},
		{
			name:   "a skipped run is not a failure",
			body:   pullRequest(`"OPEN"`, false, `null`, rollup("SUCCESS", checkRun("promises", "COMPLETED", "SKIPPED"))),
			status: forge.StatusOpen, checks: forge.ChecksGreen, review: forge.ReviewNone,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			forgeSaid := answering(one.body)
			defer forgeSaid.Close()

			reading, err := readerAgainst(forgeSaid.URL).Read(context.Background(), at(t, anAddress))
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if reading.Status != one.status {
				t.Fatalf("it reads as %q, want %q", reading.Status, one.status)
			}
			if reading.Checks != one.checks {
				t.Fatalf("the checks read as %q, want %q", reading.Checks, one.checks)
			}
			if reading.FailedCheck != one.failedCheck {
				t.Fatalf("the failed check is %q, want %q", reading.FailedCheck, one.failedCheck)
			}
			if reading.Review != one.review {
				t.Fatalf("the review reads as %q, want %q", reading.Review, one.review)
			}
		})
	}
}

// The credential goes in the header and the question names the pull request that was asked about. A
// reader that asked about the wrong number would answer confidently about somebody else's work.
func TestTheCallCarriesTheCredentialAndAsksAboutThisPullRequest(t *testing.T) {
	var authorization, asked string
	forgeSaid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		asked = string(body)
		fmt.Fprint(w, pullRequest(`"MERGED"`, true, `null`, `null`))
	}))
	defer forgeSaid.Close()

	if _, err := readerAgainst(forgeSaid.URL).Read(context.Background(), at(t, anAddress)); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if authorization != "Bearer a-token" {
		t.Fatalf("the call carried %q", authorization)
	}
	for _, want := range []string{`"owner":"atlantic-blue"`, `"name":"quay-crew"`, `"number":549`} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the question was %q, and it never says %s", asked, want)
		}
	}
}

func aToken(context.Context) (string, error) { return "a-token", nil }

func readerAgainst(endpoint string) *forge.GitHub {
	return &forge.GitHub{Token: aToken, Endpoint: endpoint}
}

func answering(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	}))
}

// pullRequest is one whole answer, in the shape the real service sends.
func pullRequest(state string, merged bool, review, rollup string) string {
	return fmt.Sprintf(`{"data":{"repository":{"pullRequest":{
		"state":%s,"merged":%t,"reviewDecision":%s,
		"commits":{"nodes":[{"commit":{"statusCheckRollup":%s}}]}}}}}`, state, merged, review, rollup)
}

func rollup(state string, contexts ...string) string {
	return fmt.Sprintf(`{"state":"%s","contexts":{"nodes":[%s]}}`, state, strings.Join(contexts, ","))
}

func checkRun(name, status, conclusion string) string {
	return fmt.Sprintf(`{"__typename":"CheckRun","name":"%s","status":"%s","conclusion":"%s"}`,
		name, status, conclusion)
}

func statusContext(context, state string) string {
	return fmt.Sprintf(`{"__typename":"StatusContext","context":"%s","state":"%s"}`, context, state)
}
