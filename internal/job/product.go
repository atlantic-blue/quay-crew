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

// Inherited is the sentence a job declared under this one carries, and the refusal where the child
// tried to state a different one.
//
// A child that says nothing carries its parent's, which is what makes the sentence reach every job in
// the tree without anybody typing it again. A child that states its own where the parent already
// carries one is refused rather than quietly ignored: a field that is dropped in silence leaves the
// caller believing the product moved, and a tree with two products has none.
//
// Under a parent that carries no sentence, the child's stands. A tree that started without one can
// still gain one, which is the path an answer of "no" takes.
func Inherited(parent, child string) (string, error) {
	carried, ok := inherited(parent, child)
	if !ok {
		return "", fmt.Errorf("this job states a different sentence from the job it hangs under, which already "+
			"says: %s. The sentence belongs to the job at the top and every job under it carries the same one, "+
			"so declare this one without it, or change the sentence at the top", parent)
	}
	return carried, nil
}

// inherited is the rule the product sentence and the request both follow: a child that says nothing
// carries its parent's, a child that says the same says the same, and a child that says something
// else is not carried at all.
//
// It is one function because the two fields answer one question. A tree with two of either has
// neither, and which noun the refusal names is the only thing that differs.
func inherited(parent, child string) (string, bool) {
	switch {
	case parent == "":
		return child, true
	case child == "" || child == parent:
		return parent, true
	default:
		return "", false
	}
}
