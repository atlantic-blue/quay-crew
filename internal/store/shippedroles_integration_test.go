//go:build integration

package store_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// The briefs the crew hands a session, read back out of the real database.
//
// The unit tier reads roles/ off disk and holds every file to the rule. What only this tier reaches
// is the crossing: a brief is written into a column by one call and read back by another, and the
// text a session is finally told to work by is what came out of that column, not what was on disk.
// A truncation, an encoding, or a read that quietly returns nothing all show here and nowhere else.

// shippedRoles is where the roles this build ships live, from this package's directory.
const shippedRoles = "../../roles"

// notOurs is the same rule the unit tier keeps, applied to what the database gives back: a name a
// brief must not carry, the prefix it put on its own agents, and the prefix its own commands took.
var notOurs = regexp.MustCompile(`(?i)greenlight|\bgl-|/gl:`)

func TestTheBriefsTheCrewHoldsNameNoOtherProduct(t *testing.T) {
	onDisk, err := role.All(shippedRoles)
	if err != nil {
		t.Fatalf("loading the roles this build ships: %v", err)
	}
	if len(onDisk) == 0 {
		t.Fatal("roles/ holds none, so this test would sweep nothing and report a pass")
	}

	s, _ := aCrewWithRoles(t, &model.FakeRunner{})
	ctx := context.Background()
	for _, one := range onDisk {
		files := make([]*quaycrewv1.RoleFile, 0, 2)
		read, err := role.ReadDir(one.Dir)
		if err != nil {
			t.Fatalf("reading %s: %v", one.Dir, err)
		}
		for _, file := range read {
			files = append(files, &quaycrewv1.RoleFile{Path: file.Path, Body: file.Body})
		}
		if _, err := s.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files}); err != nil {
			t.Fatalf("the crew refused the %s role, which ships with it: %v", one.Name, err)
		}
	}

	// Read back through the store rather than through the response to the import, because the import
	// could hand back what it was sent without the database holding any of it.
	kept, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer kept.Close()
	held, err := kept.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(held) != len(onDisk) {
		t.Fatalf("the database holds %d roles and roles/ ships %d", len(held), len(onDisk))
	}

	briefs := map[string]string{}
	for _, one := range onDisk {
		briefs[one.Name] = one.Brief
	}
	checked := 0
	for _, one := range held {
		// A brief that came back empty passes any "does not contain" check, which is the false green
		// this whole test is exposed to. So it is held to the file first, and to the rule after.
		want, known := briefs[one.Name]
		if !known {
			t.Errorf("the database holds a %s role and roles/ ships none", one.Name)
			continue
		}
		if one.Brief != want {
			t.Errorf("%s came back with a brief of %d bytes and its file is %d",
				one.Name, len(one.Brief), len(want))
			continue
		}
		checked++
		for at, line := range strings.Split(one.Brief, "\n") {
			if notOurs.MatchString(line) {
				t.Errorf("the brief the crew would hand a %s session names a product that is not quay, at line %d: %s",
					one.Name, at+1, strings.TrimSpace(line))
			}
		}
	}
	if checked != len(onDisk) {
		t.Fatalf("%d briefs came back whole and roles/ ships %d", checked, len(onDisk))
	}
	t.Logf("read %d briefs back out of the database", checked)
}

// The sad path, and the proof the rule above is doing job. A brief edited to carry the name is
// refused by this check on the far side of the database, so the guard cannot be satisfied by a read
// that returns nothing.
func TestABriefCarryingAnotherProductIsCaughtAfterTheDatabaseRoundTrip(t *testing.T) {
	s, _ := aCrewWithRoles(t, &model.FakeRunner{})
	ctx := context.Background()

	read, err := role.ReadDir(shippedRoles + "/test-writer")
	if err != nil {
		t.Fatalf("reading the test-writer role: %v", err)
	}
	files := make([]*quaycrewv1.RoleFile, 0, len(read))
	for _, file := range read {
		body := file.Body
		if file.Path == role.BriefFile {
			body = []byte(strings.Replace(string(body),
				"A flow step or a job names this role.", "You are spawned by /gl:slice.", 1))
		}
		files = append(files, &quaycrewv1.RoleFile{Path: file.Path, Body: body})
	}
	if _, err := s.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files}); err != nil {
		t.Fatalf("importing the edited role: %v", err)
	}

	kept, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer kept.Close()
	back, err := kept.GetRole(ctx, "test-writer", 1)
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if !notOurs.MatchString(back.Brief) {
		t.Fatal("the edited brief came back clean, so the check reads something other than the brief")
	}
}
