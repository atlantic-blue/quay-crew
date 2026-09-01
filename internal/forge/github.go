package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TokenName is the secret the reader is given, at the system's level.
//
// The same name the github skill already names, so an operator sets one token for one forge rather
// than two that have to be kept in step. It is the system's level rather than a workspace's because
// one process does this reading: the control plane reads every workspace's pull requests, and a
// credential that lived on a workspace would leave the crew able to read one workspace's work and
// not the next one's, for no reason a person could see.
//
//	gh auth token | krewe secret set system GH_TOKEN
const TokenName = "GH_TOKEN"

// Host is the one forge this build reads. A pull request anywhere else is reported unknown, by name,
// rather than read with a client nothing here has ever run against that service.
const Host = "github.com"

// endpoint is where the question goes.
const endpoint = "https://api." + Host + "/graphql"

// readTimeout is how long one reading may take before it is given up as unknown. A forge that is slow
// must not hold the timer that reads every other pull request.
const readTimeout = 20 * time.Second

// GitHub reads a pull request from GitHub, in one call.
//
// One call rather than four, and this is why it speaks GraphQL where the rest of the system speaks
// plain addresses: the state, the merge, the checks and the review decision hang off three different
// REST endpoints, and the checks hang off the head commit, which is a fourth read to find. The cost
// of the whole feature is one call for each unsettled pull request at each interval, and that number
// is only true here.
type GitHub struct {
	// Token is the credential, read at the moment it is needed rather than held. An operator sets it
	// while the system is running, and a reader that read it once at startup would go on reporting
	// unknown until somebody restarted the system.
	Token func(ctx context.Context) (string, error)
	// Client is the client the call goes through. Nil takes one with a timeout on it.
	Client *http.Client
	// Endpoint is where the call goes. Empty takes GitHub's. A test points it at its own server.
	Endpoint string
}

var _ Reader = (*GitHub)(nil)

// theQuery asks for everything one reading needs, about one pull request.
//
// The contexts are asked for by both shapes on purpose. A GitHub Actions job is a CheckRun and a
// service posting a commit status is a StatusContext, and a reader that asked for only the first
// would report green on a pull request a status had failed.
const theQuery = `query($owner:String!,$name:String!,$number:Int!){
  repository(owner:$owner,name:$name){
    pullRequest(number:$number){
      state
      merged
      reviewDecision
      commits(last:1){nodes{commit{statusCheckRollup{
        state
        contexts(first:100){nodes{
          __typename
          ... on CheckRun { name conclusion status }
          ... on StatusContext { context state }
        }}
      }}}}
    }
  }
}`

// Read is what GitHub says about one pull request, now.
//
// It returns an error rather than an unknown reading, and the caller turns one into the other. The
// split is deliberate: this says what happened, and whoever writes the row decides that a failure is
// recorded as unknown with its reason beside it.
func (g *GitHub) Read(ctx context.Context, at Address) (Reading, error) {
	if at.Host != Host {
		return Reading{}, fmt.Errorf("this build reads %s, and that pull request is on %s", Host, at.Host)
	}
	token, err := g.token(ctx)
	if err != nil {
		return Reading{}, err
	}

	body, err := json.Marshal(map[string]any{
		"query": theQuery,
		"variables": map[string]any{
			"owner": at.Owner, "name": at.Name, "number": at.Number,
		},
	})
	if err != nil {
		return Reading{}, fmt.Errorf("asking about %s: %w", at, err)
	}

	asked, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(asked, http.MethodPost, g.where(), bytes.NewReader(body))
	if err != nil {
		return Reading{}, fmt.Errorf("asking about %s: %w", at, err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := g.client().Do(request)
	if err != nil {
		return Reading{}, fmt.Errorf("asking about %s: %w", at, err)
	}
	defer func() { _ = response.Body.Close() }()
	// Capped, because the reason goes onto a row somebody reads and a forge that answers with a page
	// of hypertext must not put a page of hypertext there.
	answered, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if err != nil {
		return Reading{}, fmt.Errorf("asking about %s: %w", at, err)
	}
	if response.StatusCode != http.StatusOK {
		return Reading{}, fmt.Errorf("%s answered %s about %s%s",
			Host, response.Status, at, rateLimited(response))
	}
	return readAnswer(answered, at)
}

// rateLimited says the call was refused for asking too often, where the headers say so. A rate limit
// and a bad credential are both a refusal and they need different repairs, so the reason names which.
func rateLimited(response *http.Response) string {
	if response.Header.Get("X-RateLimit-Remaining") != "0" {
		return ""
	}
	return ": the rate limit is spent, so no pull request can be read until it resets"
}

// theAnswer is the shape GitHub sends back, and nothing more of it than a reading needs.
type theAnswer struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Data struct {
		Repository *struct {
			PullRequest *struct {
				State          string `json:"state"`
				Merged         bool   `json:"merged"`
				ReviewDecision string `json:"reviewDecision"`
				Commits        struct {
					Nodes []struct {
						Commit struct {
							StatusCheckRollup *struct {
								State    string `json:"state"`
								Contexts struct {
									Nodes []struct {
										TypeName   string `json:"__typename"`
										Name       string `json:"name"`
										Conclusion string `json:"conclusion"`
										Status     string `json:"status"`
										Context    string `json:"context"`
										State      string `json:"state"`
									} `json:"nodes"`
								} `json:"contexts"`
							} `json:"statusCheckRollup"`
						} `json:"commit"`
					} `json:"nodes"`
				} `json:"commits"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

// readAnswer turns what GitHub sent into a reading.
func readAnswer(body []byte, at Address) (Reading, error) {
	var answer theAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		return Reading{}, fmt.Errorf("%s answered about %s in a shape this build cannot read: %w", Host, at, err)
	}
	// GraphQL answers 200 and puts the refusal in the body, so a reader that switched on the status
	// code alone would read a refusal as an empty pull request.
	if len(answer.Errors) > 0 {
		return Reading{}, fmt.Errorf("%s refused the question about %s: %s",
			Host, at, answer.Errors[0].Message)
	}
	if answer.Data.Repository == nil || answer.Data.Repository.PullRequest == nil {
		return Reading{}, fmt.Errorf("%s holds no pull request at %s, or this credential cannot see it", Host, at)
	}

	found := answer.Data.Repository.PullRequest
	reading := Reading{
		Status: status(found.State, found.Merged),
		Review: review(found.ReviewDecision),
	}
	if len(found.Commits.Nodes) == 0 {
		// A pull request with no commit has no head to hang a check on. It is a real answer and it is
		// not a green one.
		reading.Checks = ChecksNone
		return reading, nil
	}
	rollup := found.Commits.Nodes[0].Commit.StatusCheckRollup
	if rollup == nil {
		reading.Checks = ChecksNone
		return reading, nil
	}
	reading.Checks, reading.FailedCheck = checks(rollup.State, rollup.Contexts.Nodes)
	return reading, nil
}

// status is what the pull request is. The merge flag decides over the state, because GitHub calls a
// merged pull request MERGED and a client reading only "not open" would file every merge as a close.
func status(state string, merged bool) string {
	if merged {
		return StatusMerged
	}
	switch strings.ToUpper(state) {
	case "OPEN":
		return StatusOpen
	case "MERGED":
		return StatusMerged
	case "CLOSED":
		return StatusClosed
	default:
		return StatusUnknown
	}
}

// review is what the reviews add up to. Nothing said is none rather than approved: a pull request
// nobody has looked at has not been agreed to.
func review(decision string) string {
	switch strings.ToUpper(decision) {
	case "CHANGES_REQUESTED":
		return ReviewChangesRequested
	case "APPROVED":
		return ReviewApproved
	case "", "REVIEW_REQUIRED":
		return ReviewNone
	default:
		return ReviewUnknown
	}
}

// theContext is one check or one commit status, as this reader needs it.
type theContext = struct {
	TypeName   string `json:"__typename"`
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
	Context    string `json:"context"`
	State      string `json:"state"`
}

// checks is what the checks on the head commit say together, and the name of the first one that
// failed.
//
// Red beats pending and pending beats green, which is the order that keeps the answer honest: a
// board with one failure and nine still running has already failed. A check this build cannot read
// leaves the answer unknown rather than dropping it, because a total that silently leaves one out is
// worse than a total that is missing.
func checks(rollup string, contexts []theContext) (string, string) {
	if len(contexts) == 0 {
		return fromRollup(rollup), ""
	}
	pending, green, unreadable := false, false, false
	for _, one := range contexts {
		said, name := oneCheck(one)
		switch said {
		case ChecksRed:
			return ChecksRed, name
		case ChecksPending:
			pending = true
		case ChecksGreen:
			green = true
		default:
			unreadable = true
		}
	}
	switch {
	case pending:
		return ChecksPending, ""
	case unreadable:
		return ChecksUnknown, ""
	case green:
		return ChecksGreen, ""
	default:
		return ChecksNone, ""
	}
}

// oneCheck is what one entry says, and the name to print where it failed.
func oneCheck(one theContext) (string, string) {
	if one.TypeName == "StatusContext" {
		return statusState(one.State), one.Context
	}
	// A check run that has not finished is pending whatever its conclusion field holds, because the
	// conclusion of an unfinished run is empty and empty is not a pass.
	if strings.ToUpper(one.Status) != "COMPLETED" {
		return ChecksPending, one.Name
	}
	switch strings.ToUpper(one.Conclusion) {
	case "SUCCESS", "NEUTRAL", "SKIPPED":
		return ChecksGreen, one.Name
	case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE", "STALE":
		return ChecksRed, one.Name
	default:
		return ChecksUnknown, one.Name
	}
}

// statusState is what one commit status says.
func statusState(state string) string {
	switch strings.ToUpper(state) {
	case "SUCCESS":
		return ChecksGreen
	case "PENDING", "EXPECTED":
		return ChecksPending
	case "FAILURE", "ERROR":
		return ChecksRed
	default:
		return ChecksUnknown
	}
}

// fromRollup is the answer where the forge gave a total and no entries under it.
func fromRollup(state string) string {
	switch strings.ToUpper(state) {
	case "SUCCESS":
		return ChecksGreen
	case "PENDING", "EXPECTED":
		return ChecksPending
	case "FAILURE", "ERROR":
		return ChecksRed
	case "":
		return ChecksNone
	default:
		return ChecksUnknown
	}
}

// token is the credential, and a refusal naming the command that sets it where there is none. The
// refusal is the whole value of this branch: a system with no token must say so on every row it
// could not read, rather than leaving an operator to work out why every pull request reads unknown.
func (g *GitHub) token(ctx context.Context) (string, error) {
	if g.Token == nil {
		return "", fmt.Errorf("this system holds no forge credential, so set one with "+
			"gh auth token | krewe secret set system %s", TokenName)
	}
	token, err := g.Token(ctx)
	if err != nil || strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("this system holds no %s, so set one with "+
			"gh auth token | krewe secret set system %s", TokenName, TokenName)
	}
	return strings.TrimSpace(token), nil
}

func (g *GitHub) where() string {
	if g.Endpoint != "" {
		return g.Endpoint
	}
	return endpoint
}

func (g *GitHub) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return &http.Client{Timeout: readTimeout}
}
