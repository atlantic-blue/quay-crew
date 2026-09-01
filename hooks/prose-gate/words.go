package main

// The word lists this gate carries, and what each one is for.
//
// None of them is the standard's dictionary. That is about nine hundred approved words, it is
// licensed rather than published as a list, and a gate that shipped its own guess at it would refuse
// prose in the name of a rule nobody wrote. These are three closed classes of English grammar
// instead: the irregular participles, the words that end in "ed" and are not verbs, and the words
// that end in "ing" and are not verbs. A closed class does not grow, so a list of one is a fact
// rather than an opinion.

// irregular are the past participles that do not end in "ed". Without them "had run" and "has been"
// read as an auxiliary carrying nothing, and the perfect tenses that use the commonest verbs in the
// language are the ones that go through.
var irregular = set(
	"been", "become", "begun", "bent", "bound", "bitten", "blown", "broken", "brought", "built",
	"bought", "burnt", "caught", "chosen", "come", "cost", "crept", "cut", "dealt", "done", "drawn",
	"driven", "drunk", "eaten", "fallen", "fed", "felt", "fought", "found", "flown", "forgotten",
	"forgiven", "frozen", "got", "gotten", "given", "gone", "grown", "hung", "heard", "held",
	"hidden", "hit", "hurt", "kept", "known", "laid", "lain", "learnt", "led", "left", "lent", "let",
	"lost", "made", "meant", "met", "paid", "put", "quit", "read", "ridden", "risen", "run", "said",
	"seen", "sent", "set", "shaken", "shone", "shot", "shown", "shut", "slept", "slid", "sold",
	"spoken", "spent", "split", "spread", "stood", "stolen", "struck", "stuck", "sung", "sunk",
	"sat", "swept", "sworn", "swum", "taken", "taught", "thought", "thrown", "told", "torn",
	"understood", "woken", "won", "worn", "written", "wept",
)

// notAParticiple are the words that end in "ed" and are not verbs. "It has speed" is not a tense,
// and neither is "a hundred". Short, because the shape only misreads a word that can stand directly
// after "has", "have" or "had".
var notAParticiple = set(
	"need", "speed", "seed", "feed", "deed", "greed", "creed", "breed", "weed", "bed", "red",
	"sled", "hundred", "sacred", "wicked", "naked", "shed",
)

// notAVerb are the words that end in "ing" and are not a verb when they stand directly after a form
// of "be". That is the only place this list is read, so it holds only words that can stand there.
//
// "There is nothing to prove" is the one that matters most, because it is a sentence this repository
// writes constantly, and without this list every one of them reads as a continuous tense. The
// adjectives are the other half: "the file is missing" and "the work is outstanding" are states
// rather than actions, and a writer told to put those in the simple present has been refused
// something they would argue with, which is how a gate gets turned off.
var notAVerb = set(
	// Nouns.
	"nothing", "something", "anything", "everything", "bookkeeping",
	// Adjectives.
	"missing", "outstanding", "pending", "willing", "confusing", "misleading", "interesting",
	"surprising", "promising", "challenging", "demanding", "encouraging", "disappointing",
	"striking", "leading", "remaining", "corresponding", "upcoming", "ongoing", "incoming",
	"outgoing", "underlying", "alarming", "existing", "worrying", "exciting", "binding",
)

func set(words ...string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, word := range words {
		out[word] = true
	}
	return out
}
