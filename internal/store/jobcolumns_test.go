package store

import (
	"testing"
)

// The row every read of a job selects has to match the row every read of a job scans.
//
// This is the trap a new column on this table walks into, and it costs a whole tier to find. A
// column added to the list and not to the scan, or the other way round, compiles and passes every
// test that uses the memory store: only the real engine notices, and what it does then is read the
// wrong value into the wrong field or refuse the query outright. The two lists are three hundred
// lines apart, so the one that gets forgotten is whichever one was not being edited.
//
// It counts rather than compares names, because the scan is a list of Go fields and the select is a
// list of column names and the two are deliberately not written the same. A count is what a person
// gets wrong.
func TestTheColumnsSelectedAreTheColumnsScanned(t *testing.T) {
	selected := columnsIn(jobColumns)
	if selected < 60 {
		t.Fatalf("jobColumns reads as %d columns, which is fewer than this table has: the count is "+
			"being taken wrong rather than the list being short", selected)
	}
	scanned := scannedFields(t)
	if selected != scanned {
		t.Fatalf("scanJob reads %d values out of a row of %d columns: a column in one list and not "+
			"the other reads back empty in Postgres while the memory store passes every test",
			scanned, selected)
	}
}

// columnsIn counts the columns one select asks for. A call such as coalesce(parent, ”) carries a
// comma of its own, so commas inside brackets are not separators.
func columnsIn(list string) int {
	count, depth := 1, 0
	for _, letter := range list {
		switch letter {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	return count
}

// scannedFields counts what scanJob asks a row for, by handing it a row that counts rather than
// reads. It is the same call the real engine makes, so the count is the code's rather than a
// reading of it.
func scannedFields(t *testing.T) int {
	t.Helper()
	counting := &countingRow{}
	if _, err := scanJob(counting); err != nil {
		t.Fatalf("scanJob: %v", err)
	}
	return counting.count
}

// countingRow answers every scan with nothing and remembers how many destinations it was given.
type countingRow struct{ count int }

func (c *countingRow) Scan(dest ...any) error {
	c.count = len(dest)
	return nil
}
