package console

import (
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// catalogueClient is a control plane double for the three listings of what the system holds. It
// embeds the generated interface so a call these views are not supposed to make panics loudly rather
// than being silently satisfied.
type catalogueClient struct {
	quaycrewv1.ControlPlaneServiceClient

	roles  []*quaycrewv1.Role
	skills []*quaycrewv1.Skill
	hooks  []*quaycrewv1.Hook

	// What each view asked for, so a case can say the request was the system's own catalogue rather
	// than a workspace's or a session's. The three answers are different, and only two of them carry
	// a reason a thing is held and not given.
	rolesFor  *quaycrewv1.ListRolesRequest
	skillsFor *quaycrewv1.ListSkillsRequest
	hooksFor  *quaycrewv1.ListHooksRequest

	listErr error
}

func (c *catalogueClient) ListRoles(_ context.Context, req *quaycrewv1.ListRolesRequest, _ ...grpc.CallOption) (*quaycrewv1.ListRolesResponse, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	c.rolesFor = req
	return &quaycrewv1.ListRolesResponse{Roles: c.roles}, nil
}

// The system's own listing is answered the way the control plane answers it, `left_out` included:
// it fills that field on a workspace listing and on a session listing, and never on this one. A
// double looser than the real thing is how a cell that is empty on every real row passes a test.
func (c *catalogueClient) ListSkills(_ context.Context, req *quaycrewv1.ListSkillsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListSkillsResponse, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	c.skillsFor = req
	answered := make([]*quaycrewv1.Skill, 0, len(c.skills))
	for _, one := range c.skills {
		carried := proto.Clone(one).(*quaycrewv1.Skill)
		if req.GetWorkspace() == "" && req.GetSession() == "" {
			carried.LeftOut = ""
		}
		answered = append(answered, carried)
	}
	return &quaycrewv1.ListSkillsResponse{Skills: answered}, nil
}

// Hooks carry no reason at all: nothing in the control plane fills `left_out` on a hook, on any of
// its listings, so this answers what the real one answers.
func (c *catalogueClient) ListHooks(_ context.Context, req *quaycrewv1.ListHooksRequest, _ ...grpc.CallOption) (*quaycrewv1.ListHooksResponse, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	c.hooksFor = req
	answered := make([]*quaycrewv1.Hook, 0, len(c.hooks))
	for _, one := range c.hooks {
		carried := proto.Clone(one).(*quaycrewv1.Hook)
		carried.LeftOut = ""
		answered = append(answered, carried)
	}
	return &quaycrewv1.ListHooksResponse{Hooks: answered}, nil
}

func aCatalogue() *catalogueClient {
	return &catalogueClient{
		roles: []*quaycrewv1.Role{{
			Name: "releaser", Version: 3, Model: "opus", System: true,
			Summary: "ships the work a job finished",
		}},
		skills: []*quaycrewv1.Skill{{
			Name: "github", Version: 2, System: true, Binaries: []string{"gh", "git"},
			Summary: "open pull requests and issues with gh",
		}},
		hooks: []*quaycrewv1.Hook{{
			Name: "test-gate", Version: 4, System: true,
			Summary: "a change to the code carries its tests",
			Events:  []*quaycrewv1.HookBinding{{On: "PreToolUse", Matcher: "Write"}},
		}},
	}
}

// Each of the three lists what the control plane holds, on the screen an operator lives in. Until
// this, the answer was on the command line and nowhere else.
func TestEachHoldingListsWhatTheSystemHolds(t *testing.T) {
	client := aCatalogue()
	for _, view := range []struct {
		name  string
		build func() Resource
		says  []string
	}{
		{"roles", func() Resource { return Roles(client) }, []string{"releaser", "v3", "opus", "ships the work"}},
		{"skills", func() Resource { return Skills(client) }, []string{"github", "v2", "gh git", "open pull requests"}},
		{"hooks", func() Resource { return Hooks(client) }, []string{"test-gate", "v4", "PreToolUse(Write)", "carries its tests"}},
	} {
		t.Run(view.name, func(t *testing.T) {
			resource := view.build()
			rows, err := resource.List(context.Background(), "")
			if err != nil {
				t.Fatalf("listing %s: %v", view.name, err)
			}
			if len(rows) != 1 {
				t.Fatalf("the %s view lists %d rows, want the one the system holds", view.name, len(rows))
			}
			// On the screen rather than in the row, because a cell the table drops is a cell nobody
			// reads.
			model := newTestModel(t, resource)
			model.width = 140
			model, _ = update(t, model, rowsFor(model, rows...))
			drawn := model.View()
			for _, want := range view.says {
				if !strings.Contains(drawn, want) {
					t.Fatalf("the %s listing does not carry %q:\n%s", view.name, want, drawn)
				}
			}
		})
	}
}

// A system holding none of them is a real state, and the screen says so rather than leaving a panel
// that reads as a console that failed to draw.
func TestEachHoldingSaysWhenTheSystemHoldsNothing(t *testing.T) {
	empty := &catalogueClient{}
	for _, view := range []struct {
		name  string
		build func() Resource
	}{
		{"roles", func() Resource { return Roles(empty) }},
		{"skills", func() Resource { return Skills(empty) }},
		{"hooks", func() Resource { return Hooks(empty) }},
	} {
		t.Run(view.name, func(t *testing.T) {
			resource := view.build()
			rows, err := resource.List(context.Background(), "")
			if err != nil {
				t.Fatalf("listing %s: %v", view.name, err)
			}
			if len(rows) != 0 {
				t.Fatalf("the empty %s view lists %d rows", view.name, len(rows))
			}
			model := newTestModel(t, resource)
			model, _ = update(t, model, rowsFor(model))
			if !strings.Contains(model.View(), "nothing here") {
				t.Fatalf("the empty %s view says nothing about being empty:\n%s", view.name, model.View())
			}
		})
	}
}

func TestEachHoldingSurfacesTheControlPlaneError(t *testing.T) {
	client := &catalogueClient{listErr: errors.New("unavailable")}
	for name, resource := range map[string]Resource{
		"roles": Roles(client), "skills": Skills(client), "hooks": Hooks(client),
	} {
		if _, err := resource.List(context.Background(), ""); err == nil {
			t.Fatalf("the %s view swallowed the control plane error", name)
		}
	}
}

// The question an operator has in front of these listings, and the one a name and a version cannot
// answer: a workspace nobody attached anything to still holds four skills, and this says why.
func TestAHoldingSaysHowFarItReaches(t *testing.T) {
	client := aCatalogue()
	client.skills = append(client.skills, &quaycrewv1.Skill{
		Name: "aws", Version: 1, Summary: "read cloud state",
	})

	rows, err := Skills(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing skills: %v", err)
	}
	// The literals rather than the constants: a case reading the constant passes against it emptied
	// out, which is the one mistake this is here to catch.
	if got := rows[0].Cells[2]; got != "everywhere" {
		t.Fatalf(`a skill the system holds reads %q, want "everywhere"`, got)
	}
	if got := rows[1].Cells[2]; got != "on attach" {
		t.Fatalf(`a skill only an attachment gives out reads %q, want "on attach"`, got)
	}
}

// The three views ask for the system's own catalogue, and the control plane fills `left_out` only on a
// workspace listing and on a session listing. A double that answered with a reason would let a cell
// pass here that is empty on every row in front of an operator.
func TestTheseViewsAskForWhatTheSystemHoldsAndNothingNarrower(t *testing.T) {
	client := aCatalogue()

	for _, resource := range []Resource{Roles(client), Skills(client), Hooks(client)} {
		if _, err := resource.List(context.Background(), ""); err != nil {
			t.Fatalf("listing %s: %v", resource.Name, err)
		}
	}
	if client.skillsFor.GetWorkspace() != "" || client.skillsFor.GetSession() != "" {
		t.Fatalf("the skills view asked for workspace %q session %q, want the system's own catalogue",
			client.skillsFor.GetWorkspace(), client.skillsFor.GetSession())
	}
	if client.hooksFor.GetWorkspace() != "" || client.hooksFor.GetSession() != "" {
		t.Fatalf("the hooks view asked for workspace %q session %q, want the system's own catalogue",
			client.hooksFor.GetWorkspace(), client.hooksFor.GetSession())
	}
	if client.rolesFor.GetWorkspace() != "" {
		t.Fatalf("the roles view asked for workspace %q, want the system's own catalogue", client.rolesFor.GetWorkspace())
	}
}

// A hook bound to every tool says the event alone. "PreToolUse()" reads as a matcher that failed to
// print rather than as a hook that fires for everything.
func TestAHookBoundToEveryToolNamesTheEventAlone(t *testing.T) {
	got := firesOn(&quaycrewv1.Hook{Events: []*quaycrewv1.HookBinding{
		{On: "UserPromptSubmit"}, {On: "PreToolUse", Matcher: "Bash"},
	}})
	if got != "UserPromptSubmit PreToolUse(Bash)" {
		t.Fatalf("the events read %q", got)
	}
}

func TestTheHoldingViewsAreRegisteredAndAnswerToWhatFingersType(t *testing.T) {
	registry, err := NewDefaultRegistry(aCatalogue())
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	for typed, want := range map[string]string{
		"roles": "roles", "role": "roles",
		"skills": "skills", "skill": "skills",
		"hooks": "hooks", "hook": "hooks",
	} {
		resource, found := registry.Resolve(typed)
		if !found {
			t.Fatalf("typing %q opens nothing", typed)
		}
		if resource.Name != want {
			t.Fatalf("typing %q opens %q, want %q", typed, resource.Name, want)
		}
	}
}
