package job

import "fmt"

// A job carries the one sentence it serves, and every job under it carries the same one.
//
// The sentence says what a person does with what is built and what they get back, in the words that
// person would use. It is not the architecture and it is not the address shape.
//
// It exists because a design document is evidence for the product and not the product itself. A run
// built a document faithfully, every check was green, and the operator opened it two days later and
// could not use it: section 3 said the address reads `/videos?id=<video id>`, so every job downstream
// took the video identifier as the key, and a reader holding a link had to extract that identifier by
// hand before the page was any use. Nobody had written "paste a link and get the text back", so
// nothing was ever measured against it.

// ServesAPerson is the line a session doing this job is given above its brief.
//
// It says the sentence wins over the brief, because that is the whole point: a session that finds the
// design and the sentence disagreeing is asked to say so rather than to build the design faithfully.
// Building it faithfully is what already happened.
func ServesAPerson(sentence string) string {
	return fmt.Sprintf("This job serves one sentence, which is what a person does with what you build and "+
		"what they get back: %s. The brief below and any design it names are evidence for that sentence, "+
		"never a replacement for it. Where the two disagree, the sentence wins: say so in your answer "+
		"rather than building the design as written.", sentence)
}
