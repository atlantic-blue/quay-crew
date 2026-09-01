package console

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
)

// The console is where an operator looks first, so a project that ships somewhere says so in the
// projects view rather than only through a command somebody has to know to type.
func TestTheProjectsViewSaysWhereEachProjectDeploys(t *testing.T) {
	client := &fakeClient{
		workspaces: []*quaycrewv1.Workspace{{Id: "acme", Name: "acme"}},
		projects: []*quaycrewv1.Project{
			{Id: "p1", Workspace: "acme", Name: "house-bills", DeployTarget: &quaycrewv1.DeployTarget{
				Account:  "123456789012",
				Region:   "eu-west-2",
				Identity: "arn:aws:iam::123456789012:role/krewe-deploy",
			}},
			{Id: "p2", Workspace: "acme", Name: "gardening"},
		},
	}
	projects := Projects(client)

	headings := make([]string, 0, len(projects.Columns))
	for _, column := range projects.Columns {
		headings = append(headings, column.Title)
	}
	if !contains(headings, "deploys to") {
		t.Fatalf("the projects view has no column saying where a project ships: %v", headings)
	}

	rows, err := projects.List(context.Background(), "acme")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the view has %d rows, want 2", len(rows))
	}
	shipping, silent := rows[0], rows[1]
	if shipping.ID != "p1" {
		shipping, silent = rows[1], rows[0]
	}
	if !strings.Contains(strings.Join(shipping.Cells, " "), "123456789012/eu-west-2") {
		t.Fatalf("the row for a project that ships says %v", shipping.Cells)
	}
	// A project that has not said carries an empty cell rather than a word that reads like an
	// account, so the column answers "which of these has been told" at a glance.
	for _, cell := range silent.Cells {
		if strings.Contains(cell, "/") && strings.Contains(cell, "1234") {
			t.Fatalf("a project that has not said reads as deploying somewhere: %v", silent.Cells)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
