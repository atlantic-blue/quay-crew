package display

import (
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// Three names can be on a session and a listing has one cell, so the order they are read in decides
// what an operator sees. The label is what the person who has seen the session called it, the title
// is what they typed when they declared the job, and the description is what a model wrote.
func TestWhatASessionIsCalled(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		label       string
		title       string
		description string
		want        string
	}{
		{
			name:  "the title alone, which is the whole of a running job's name",
			title: "read the electricity bill",
			want:  "read the electricity bill",
		},
		{
			// The one this is here to protect. The operator renamed the conversation after seeing it,
			// so their word is the last one whatever the job was declared as.
			name:  "a label the operator set beats the title it was dispatched with",
			label: "the bill that is actually overdue",
			title: "read the electricity bill",
			want:  "the bill that is actually overdue",
		},
		{
			name:        "the title beats a description a model wrote",
			title:       "read the electricity bill",
			description: "an engaging exploration of household paperwork",
			want:        "read the electricity bill",
		},
		{
			name:        "a label beats both",
			label:       "the bill that is actually overdue",
			title:       "read the electricity bill",
			description: "an engaging exploration of household paperwork",
			want:        "the bill that is actually overdue",
		},
		{
			name:        "the description still names a session nobody dispatched with a title",
			description: "fixing the payout job",
			want:        "fixing the payout job",
		},
		{
			name: "none of the three leaves the cell empty",
			want: "",
		},
		{
			// Space is invisible, so a title of nothing but space must read as no title rather than
			// as a name that pushes the description off the cell.
			name:        "a title of nothing but space is no title",
			title:       "   ",
			description: "fixing the payout job",
			want:        "fixing the payout job",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			session := &quaycrewv1.Session{
				Id:          "5d013d07b9bcc8c05a1f437a",
				Label:       testCase.label,
				Title:       testCase.title,
				Description: testCase.description,
			}
			if got := SessionLabel(session); got != testCase.want {
				t.Fatalf("the session is called %q, want %q", got, testCase.want)
			}
		})
	}
}

// The name cell and the breadcrumb read the same three names, and only the breadcrumb falls back to
// the identifier: a listing already prints the identifier in the session column.
func TestASessionWithNoNameAtAllFallsBackToItsIdentifier(t *testing.T) {
	unnamed := &quaycrewv1.Session{Id: "5d013d07b9bcc8c05a1f437a"}

	if got := SessionName(unnamed); got != "5d013d07" {
		t.Fatalf("an unnamed session stands as %q, want its short identifier", got)
	}
	if got := SessionName(&quaycrewv1.Session{Id: "5d013d07b9bcc8c05a1f437a", Title: "read the electricity bill"}); got != "read the electricity bill" {
		t.Fatalf("a session dispatched with a title stands as %q", got)
	}
}

// The cell a listing draws, not only the function behind it: the name column is where four blank
// cells were read off a screen of running jobs.
func TestTheNameCellOfAListingCarriesTheTitle(t *testing.T) {
	cells := SessionCells(&quaycrewv1.Session{
		Id: "5d013d07b9bcc8c05a1f437a", Status: "running", Title: "read the electricity bill",
	}, "acme", "house-bills")

	const nameColumn = 3
	if cells[nameColumn] != "read the electricity bill" {
		t.Fatalf("the name cell says %q", cells[nameColumn])
	}
	if SessionColumns()[nameColumn] != "name" {
		t.Fatalf("column %d is %q, so this test is reading the wrong cell", nameColumn, SessionColumns()[nameColumn])
	}
}
