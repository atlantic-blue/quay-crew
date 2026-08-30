package controlplane_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/capacity"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
)

// The two reserve settings were named after the word this level used to take, and a configuration
// file is written once and kept for a year.
//
// Dropping the old names would put the operator's floor silently back at the default, on the one
// setting whose whole job is to stop the machine going down under its own work. So the old names are
// still read, and the control plane says out loud that they moved.
func TestTheReserveSettingsAreStillReadUnderTheNamesTheyHad(t *testing.T) {
	t.Setenv("QC_CREW_RESERVE_MEMORY", "4096")
	t.Setenv("QC_CREW_RESERVE_PROCESSOR", "300")

	said := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(said, nil))

	reserve := controlplane.EnvReserve(logger)
	if want := int64(4096) << 20; reserve.Memory != want {
		t.Fatalf("the memory floor is %d, want %d: the setting was not read under the name it had", reserve.Memory, want)
	}
	if reserve.Processor != 300 {
		t.Fatalf("the processor floor is %d, want 300", reserve.Processor)
	}
	// Read is not enough. A setting silently carried forever is a setting nobody moves.
	for _, want := range []string{"QC_CREW_RESERVE_MEMORY", "QC_SYSTEM_RESERVE_MEMORY", "renamed"} {
		if !bytes.Contains(said.Bytes(), []byte(want)) {
			t.Fatalf("nothing said %q, so the operator is never told the name moved:\n%s", want, said)
		}
	}
}

// The new name wins where both are set, because that is the one that was typed on purpose.
func TestTheNameTheSettingHasNowWins(t *testing.T) {
	t.Setenv("QC_CREW_RESERVE_MEMORY", "4096")
	t.Setenv("QC_SYSTEM_RESERVE_MEMORY", "8192")

	reserve := controlplane.EnvReserve(nil)
	if want := int64(8192) << 20; reserve.Memory != want {
		t.Fatalf("the memory floor is %d, want %d", reserve.Memory, want)
	}
}

// Neither set leaves the floor this build measured for itself, rather than zero.
func TestNeitherNameSetLeavesTheFloorAlone(t *testing.T) {
	t.Setenv("QC_CREW_RESERVE_MEMORY", "")
	t.Setenv("QC_SYSTEM_RESERVE_MEMORY", "")
	t.Setenv("QC_CREW_RESERVE_PROCESSOR", "")
	t.Setenv("QC_SYSTEM_RESERVE_PROCESSOR", "")

	if reserve, want := controlplane.EnvReserve(nil), capacity.DefaultReserve(); reserve != want {
		t.Fatalf("the floor is %+v, want %+v", reserve, want)
	}
}
