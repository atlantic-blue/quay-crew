package capacity

// What one sandbox asks for, and what the system keeps back for itself.
//
// The request is measured. One sandbox on a fourteen processor Linux runtime with 23,997 mebibytes
// was read every two seconds through its own accounting while it did the work this system's sandboxes
// do: a clone, a cold `go build ./...` and the full test suite.
//
//	memory:    memory.current less inactive_file from memory.stat, which is the figure docker stats
//	           prints for a container
//	processor: usage_usec from cpu.stat, differenced over each interval
//
// Over 808 samples of ordinary work, reading and editing files, it held 1,159 mebibytes on average
// and 17 per cent of one processor. Through the 48 second build and test run it peaked at 1,856
// mebibytes, averaged 254 per cent of a processor and reached 931 per cent in one two second sample,
// which is nine of the fourteen processors inside one container. Against that, the ten sandboxes the
// operator measured with docker stats on the machine that went down held 4.3 to 764.5 megabytes,
// because most of a sandbox's life is spent waiting on a model rather than compiling.
//
// So the memory request is 1,536 mebibytes: above every sandbox in that listing and above what one
// holds doing ordinary work, below the peak of a build. The processor request is one whole
// processor, which is between the two measurements and nowhere near the peak, because a request the
// size of a compile would host four sandboxes on a fourteen processor machine and every one of them
// would sit idle most of the time.
//
// Both are requests and neither is a ceiling. The gap between 1,536 and the 1,856 measured here is
// exactly what a limit closes, and a limit is issue 477.
//
// Provisional, and this is what replaces them: the ninety fifth percentile of what a sandbox holds
// over the first fifty jobs a system runs, which needs the per sandbox figure the room view already
// draws to be recorded rather than only drawn.
const (
	// RequestMemory is the memory one sandbox asks for.
	RequestMemory = int64(1536) << 20
	// RequestProcessor is the processor share one sandbox asks for, in per cent of one processor.
	RequestProcessor = OneProcessor
)

// DefaultRequest is what a sandbox asks for where nothing said otherwise.
func DefaultRequest() Request {
	return Request{Memory: RequestMemory, Processor: RequestProcessor}
}

// The floor under what the system holds back for itself.
//
// This is the one number here that is not measured, and it is a floor rather than the answer. The
// reserve that binds is read from the runtime on every sample: what every container holds, less what
// the sandboxes hold, is the system's own containers, and MeasuredReserve returns whichever of the two
// is larger. The floor covers the minutes after a restart, when the system's own containers are cold
// and reading them would reserve almost nothing.
//
// The figure below stands for a control plane, a database, an event log, a collector, a gateway and
// four observability services, which is what deploy/docker-compose.yml starts. Nothing here measured
// them: this was written inside a sandbox, which cannot see them. The command that replaces it is
// one line on the machine the system runs on, with the system up and no jobs running:
//
//	docker stats --no-stream --format '{{.Name}}\t{{.MemUsage}}\t{{.CPUPerc}}' | grep -v quaycrew-
//
// Set QC_SYSTEM_RESERVE_MEMORY and QC_SYSTEM_RESERVE_PROCESSOR from what it says.
const (
	// ReserveMemory is the memory floor under the system's own containers.
	ReserveMemory = int64(2048) << 20
	// ReserveProcessor is the processor floor under them, in per cent of one processor.
	ReserveProcessor = 2 * OneProcessor
)

// DefaultReserve is the floor under what the system keeps for itself.
func DefaultReserve() Request {
	return Request{Memory: ReserveMemory, Processor: ReserveProcessor}
}
