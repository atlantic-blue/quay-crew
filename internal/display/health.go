package display

// The parts of a crew a health probe writes to.
//
// A name is what joins a probe to the row it fills, so it is one word in one place rather than a
// literal at each end and a translation table between them. It travels on the wire, so it is the
// same word the proto's HealthComponent documents.
const (
	// HealthStore is the store taking a row, which is the first write a dispatch makes.
	HealthStore = "store"
	// HealthEvents is the event log taking a record, which is the second.
	HealthEvents = "events"
)

// The words a part's state is said in.
//
// They are words rather than a boolean because there are four answers and a boolean holds two. The
// event log that died on 29 August 2026 was configured and answering nothing for sixteen hours, and
// it read exactly like a log that was working; a crew with no log at all reads different again, and
// a part nothing probes is a fourth thing. Every one of them is said out loud.
const (
	// HealthServing is a probe that landed.
	HealthServing = "serving"
	// HealthDown is a probe that did not.
	HealthDown = "down"
	// HealthNotConfigured is a part this crew has none of. Writing to nowhere succeeds, so a crew
	// recording nothing must say so rather than being read as one in good health.
	HealthNotConfigured = "not configured"
	// HealthNotChecked is a part no probe covers, and every part of a crew that has never probed. It
	// is written out rather than left blank: the finding this vocabulary answers is a dead component
	// drawn in the same colour as a healthy one, and a blank cell is that failure with fewer letters.
	HealthNotChecked = "not checked"
)
