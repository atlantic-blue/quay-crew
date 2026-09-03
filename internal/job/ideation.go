package job

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A job that states the sentence says what it understood before it writes a plan, and a person
// answers in their own words.
//
// The reading is as long as the work needs. The three numbers below are guides a session is asked to
// write to, and no longer ceilings the reader refuses text at: a reading over one of them is kept
// word for word and reaches the person who asked for it. Job a3d72b11 wrote a correct 859 byte
// reading against a guide of 600, and the reading was refused, asked for a second time, and the job
// was stopped with nobody having read a word of it. Ten million tokens went to that job and nothing
// was delivered. The length of a reading belongs to the person who reads it.
//
// The plan gate already stops a job before any work, and it stopped it one step too late. The
// session read a sentence, read a brief, and wrote seven steps out of whatever it had made of the
// two. Nobody was ever asked what the sentence meant, so the plan was the session marking its own
// reading, and a person answering "yes" to it was approving steps built on an understanding they had
// never seen. A misreading survives that gate intact, because the steps are consistent with the
// misreading.
//
// So the session writes down what it understands the work to be, what the work is not, what it does
// not know, and which parts of its understanding came from the person and which it filled in itself.
// Those last two read the same on a row today and they are not the same thing. Then it asks what it
// genuinely cannot answer from the repository, the brief and the sentence, and it stops.
//
// The answer is content rather than consent. A plan is approved by one word; this is answered in
// prose, the prose is kept whole, and the plan is then written against it. A question the answer
// leaves untouched stays unknown rather than becoming an assumption nobody made.
const (
	// IdeationQuestions is how many questions one of these is expected to carry.
	//
	// Low for the reason the plan's guide is low: a person reads this in a terminal while doing
	// something else, and a list of fifteen questions is a list nobody answers. Five is chosen rather
	// than measured. What replaces it is the distribution of questions a person actually answers,
	// which the record now holds: after fifty of these, the ninety fifth percentile of questions
	// answered is the number.
	//
	// A sixth question is kept and warned about, because the sixth one is worth reading.
	IdeationQuestions = 5
	// IdeationPoints is how many lines each of the lists is expected to carry: what was told, what
	// was assumed, and what is not known. A sixth line is kept and warned about too.
	IdeationPoints = 5
	// IdeationLineLimit is the guide for one of those lines. It is the title's guide, because both are
	// one line a person reads.
	IdeationLineLimit = TitleLimit
	// UnderstandingLimit is the guide for what the work is, and for what it is not. Wider than a line
	// and narrower than a brief: it is the paragraph a person reads first.
	UnderstandingLimit = 3 * TitleLimit
	// IdeationLimit is the guide for the whole record once the system renders it.
	//
	// The three above are guides and not ceilings. A guide tells a person that a part of the record is
	// long, and by how much. It takes nothing away. Text over a guide reaches the person word for
	// word, with a warning above it, because the record is the only thing this stage produces.
	IdeationLimit = 3000
)

// ideationLine and ideationQuestion are the shapes the system asks for and reads back, the way a plan
// is read off a reply rather than reported. What it finds is then what the session meant to say,
// rather than a sentence that happened to hold the word.
var (
	ideationLine = regexp.MustCompile(
		`(?im)^[ \t]*(understood|not|told|assumed|unknown|confidence)[ \t]*[:.][ \t]*(.+?)[ \t]*$`)
	ideationQuestion = regexp.MustCompile(`(?im)^[ \t]*question[ \t]+(\d+)[ \t]*[:.][ \t]*(.+?)[ \t]*$`)
)

// askingToProceed is what a request for permission looks like. A question that asks whether to go on
// is not a question: the whole record is already a stop, and a session that spends one of its five
// asking for permission has asked a person nothing.
//
// Narrow on purpose, and narrow in both directions. It matches a request to start rather than the
// word itself, so "which environment does the deploy proceed against" is a question and "shall I
// proceed" is not. It leaves "do you want me to include the panel" alone: that reads as permission
// and it is a question about scope, and a refusal that catches those costs a person the answer.
var askingToProceed = regexp.MustCompile(
	`(?i)(shall|should|can|may|ok|okay)[ \t]+(i|we)[ \t]+(proceed|start|begin|go ahead|carry on)|` +
		`(ok|okay|fine|happy)[ \t]+(to|for)[ \t]+(proceed|me to start|us to start)`)

// IdeationQuestion is one thing the machine cannot answer for itself, and the number an answer
// accounts for it by.
type IdeationQuestion struct {
	Number int
	Text   string
}

// Ideation is what a session understood before it planned.
//
// Told and Assumed are the point of the whole record. Told is what the person who declared the job
// stated, and Assumed is what the session filled in for itself, and today those two read the same on
// the row: a plan carries no mark saying which of its footings a human put there.
type Ideation struct {
	Understood string
	NotThis    string
	Told       []string
	Assumed    []string
	Unknown    []string
	Confidence string
	Questions  []IdeationQuestion
}

// ReadIdeation is what a reply says it understood, and the refusal where it says nothing the system
// can read.
//
// Read off the reply rather than reported, the way a plan is. A reply the system cannot read a record
// out of is prose about understanding, and putting that in front of a person is the compression fault
// this exists to catch, one level further up.
func ReadIdeation(reply string) (Ideation, error) {
	var one Ideation
	for _, found := range ideationLine.FindAllStringSubmatch(reply, -1) {
		said := TidySentence(found[2])
		if said == "" {
			continue
		}
		switch strings.ToLower(found[1]) {
		case "understood":
			one.Understood = firstOf(one.Understood, said)
		case "not":
			one.NotThis = firstOf(one.NotThis, said)
		case "told":
			one.Told = append(one.Told, said)
		case "assumed":
			one.Assumed = append(one.Assumed, said)
		case "unknown":
			one.Unknown = append(one.Unknown, said)
		case "confidence":
			one.Confidence = firstOf(one.Confidence, said)
		}
	}
	questions, err := readIdeationQuestions(reply)
	if err != nil {
		return Ideation{}, err
	}
	one.Questions = questions
	if err := one.readable(); err != nil {
		return Ideation{}, err
	}
	return one, nil
}

// firstOf keeps the first thing said under a heading. A reply that says what it understood twice is
// a reply that changed its mind in the middle, and the system reads the first rather than choosing.
func firstOf(held, said string) string {
	if held != "" {
		return held
	}
	return said
}

// readIdeationQuestions is the questions a reply carries, numbered from one with none missing and
// none repeated, because the numbers are what an answer accounts for them by.
func readIdeationQuestions(reply string) ([]IdeationQuestion, error) {
	found := ideationQuestion.FindAllStringSubmatch(reply, -1)
	questions := make([]IdeationQuestion, 0, len(found))
	for _, one := range found {
		number, err := strconv.Atoi(one[1])
		if err != nil {
			return nil, fmt.Errorf("question %q is not numbered with a number", one[1])
		}
		text := TidySentence(one[2])
		if text == "" {
			continue
		}
		if where := askingToProceed.FindString(text); where != "" {
			return nil, fmt.Errorf("question %d asks %q, which asks whether to go on rather than asking "+
				"something you cannot answer: the record already stops the job, so ask about the work",
				number, where)
		}
		questions = append(questions, IdeationQuestion{Number: number, Text: text})
	}
	sort.SliceStable(questions, func(i, j int) bool { return questions[i].Number < questions[j].Number })
	for i, one := range questions {
		if one.Number != i+1 {
			return nil, fmt.Errorf("these questions are numbered %s: number them from 1 upwards with none "+
				"missing and none repeated, because an answer opens with the number it answers",
				numbersAsked(questions))
		}
	}
	return questions, nil
}

// numbersAsked is the numbering a set of questions carried, for a refusal that has to show it.
func numbersAsked(questions []IdeationQuestion) string {
	said := make([]string, 0, len(questions))
	for _, one := range questions {
		said = append(said, strconv.Itoa(one.Number))
	}
	return strings.Join(said, ", ")
}

// readable is every rule the record is held to, in one place, and the refusal that teaches the shape.
//
// Shape rather than size. Each rule here is about something a reader cannot work with at all: a
// record with no understanding in it, a record that excludes nothing, a record that asks a person
// nothing. Length is not one of them, because text a person can read is text the system keeps.
func (one Ideation) readable() error {
	switch {
	case one.Understood == "":
		return fmt.Errorf("this reply says nothing the system can read as an understanding: "+
			"write one line each opening with %s, %s, %s and %s, and one line per question opening with %s",
			"Understood:", "Not:", "Confidence:", "Unknown:", "Question 1:")
	case one.NotThis == "":
		return fmt.Errorf("this reply says what the work is and never what it is not: write a line " +
			"opening with \"Not:\", because the reading that goes wrong is the one nothing excluded")
	case one.Confidence == "":
		return fmt.Errorf("this reply says how it understood the work and not how sure it is: write a " +
			"line opening with \"Confidence:\", in your own words. Nothing is compared against it, and a " +
			"person reads it")
	case len(one.Questions) == 0:
		return fmt.Errorf("this reply asks nothing: ask at least one thing you cannot answer from the " +
			"repository, the brief and the sentence, opening the line with \"Question 1:\". A run that " +
			"asked a person nothing is the failure this stage exists for")
	}
	return nil
}

// IdeationText is the record as the system keeps it, in the shape it reads back.
//
// The system's own rendering rather than the reply, for the reason a plan is kept that way: what a
// person reads and what the session is later handed are then the same lines, and the reasoning a
// model wraps around its answer is what makes a record as expensive to read as the work.
func IdeationText(one Ideation) string {
	lines := []string{"Understood: " + one.Understood, "Not: " + one.NotThis}
	for _, said := range one.Told {
		lines = append(lines, "Told: "+said)
	}
	for _, said := range one.Assumed {
		lines = append(lines, "Assumed: "+said)
	}
	for _, said := range one.Unknown {
		lines = append(lines, "Unknown: "+said)
	}
	lines = append(lines, "Confidence: "+one.Confidence)
	for _, said := range one.Questions {
		lines = append(lines, fmt.Sprintf("Question %d: %s", said.Number, said.Text))
	}
	return strings.Join(lines, "\n")
}

// IdeationWarnings is one line for each part of a record that is longer than its guide, and nothing
// where every part is inside its guide.
//
// A guide used to refuse the text. Job a3d72b11 wrote a correct reading of 859 bytes against a guide
// of 600, and the system threw the words away, asked once more and stopped the job. So the guide
// tells the operator instead: which part is long, how many bytes it is, and what the guide is. That
// is what the operator needs to say "that is fine" or "say it shorter next time".
//
// It measures the record the system rendered, because that is the text the person reads. A line of
// the record is a heading, a colon and what was said, so the measurement is of what was said.
func IdeationWarnings(understanding string) []string {
	var said []string
	for _, line := range strings.Split(understanding, "\n") {
		heading, text, found := strings.Cut(line, ": ")
		if !found {
			continue
		}
		switch {
		case isAParagraphOfTheRecord(heading):
			if len(text) > UnderstandingLimit {
				said = append(said, aWarningAbout(theNameOf(heading), len(text), UnderstandingLimit))
			}
		case isALineOfTheRecord(heading):
			if len(text) > IdeationLineLimit {
				said = append(said, aWarningAbout(theNameOf(heading), len(text), IdeationLineLimit))
			}
		}
	}
	said = append(said, theCountWarnings(understanding)...)
	if len(understanding) > IdeationLimit {
		said = append(said, aWarningAbout("The whole record", len(understanding), IdeationLimit))
	}
	return said
}

// theCountWarnings is one line for each list of the record that carries more lines than its guide.
//
// A sixth question used to be refused, which threw away the other five with it. A sixth thing a
// session was told is worth reading, so the count is a guide as well: the operator is told there are
// more than the guide, and reads them.
func theCountWarnings(understanding string) []string {
	counted := map[string]int{}
	for _, line := range strings.Split(understanding, "\n") {
		heading, _, found := strings.Cut(line, ": ")
		if !found {
			continue
		}
		if strings.HasPrefix(heading, "Question ") {
			heading = "Question"
		}
		counted[heading]++
	}
	var said []string
	for _, heading := range []string{"Told", "Assumed", "Unknown"} {
		if counted[heading] > IdeationPoints {
			said = append(said, fmt.Sprintf("The record carries %d %s lines where the guide is %d.",
				counted[heading], heading, IdeationPoints))
		}
	}
	if counted["Question"] > IdeationQuestions {
		said = append(said, fmt.Sprintf("The record asks %d questions where the guide is %d.",
			counted["Question"], IdeationQuestions))
	}
	return said
}

// aWarningAbout is one warning. It carries three things: which part of the record is long, how many
// bytes that part is, and the guide it went past. Two numbers with no part named leave the operator
// counting to find which part to shorten.
func aWarningAbout(part string, size, guide int) string {
	return fmt.Sprintf("%s is %d bytes where the guide is %d.", part, size, guide)
}

// isAParagraphOfTheRecord is whether a heading opens one of the two paragraphs a person reads first.
// Those two are measured against the wider guide.
func isAParagraphOfTheRecord(heading string) bool {
	return heading == "Understood" || heading == "Not"
}

// isALineOfTheRecord is whether a heading opens one line of the record.
func isALineOfTheRecord(heading string) bool {
	switch heading {
	case "Told", "Assumed", "Unknown", "Confidence":
		return true
	}
	return strings.HasPrefix(heading, "Question ")
}

// theNameOf is how a warning names the part it measured, in the words the record itself uses, so the
// operator reads the heading here and finds the same heading below.
func theNameOf(heading string) string {
	switch heading {
	case "Understood", "Not":
		return "The " + heading + " paragraph"
	case "Confidence":
		return "The Confidence line"
	}
	return "A " + heading + " line"
}

// TheWarningsAbove is the warnings as a person reads them, above the record they are about, and an
// empty string where there is nothing to warn about.
//
// Above rather than below: a measurement at the far end of a long record is a measurement the reader
// meets after the thing it was about.
func TheWarningsAbove(understanding string) string {
	warnings := IdeationWarnings(understanding)
	if len(warnings) == 0 {
		return ""
	}
	return "Some of this is longer than its guide. Nothing was cut, and every word is below.\n" +
		strings.Join(warnings, "\n") + "\n\n"
}

// IdeationIn is the record a kept row holds. A record the system wrote always reads back, so one that
// does not is empty rather than an error: nothing downstream of a person's answer can act on a
// refusal.
func IdeationIn(kept string) Ideation {
	one, err := ReadIdeation(kept)
	if err != nil {
		return Ideation{}
	}
	return one
}

// Ideated says whether a person has answered what this job understood.
//
// The answer itself is the fact, so there is no flag beside it. A plan needs one because approval is
// one word and a plan that was refused carries the same text as a plan nobody read. This is prose a
// person wrote, and prose that is there was written by somebody.
func Ideated(one *Job) bool {
	return one != nil && one.IdeationAnswer != ""
}

// WaitingForItsIdeation says whether this job still owes a person what it understood.
//
// The same gate the plan uses, by the same person: the sentence is the trigger, a job under another
// is one part of a plan somebody already approved, and stopping at every job in a tree puts a person
// back in the loop for all of them.
//
// A job whose plan a person already approved is past this. It has to be: rows written before this
// existed carry an approved plan and no reading, and a gate that read those as owing one would drag
// work a person had already agreed to back to the beginning.
func WaitingForItsIdeation(one *Job) bool {
	return Planned(one) && !Ideated(one) && !one.PlanApproved
}

// SayWhatYouUnderstood is the first task a planned job's session is given. It asks for the reading and
// for no plan and no work.
//
// The sentence goes first and the request whole underneath it, because this stage is about what was
// asked for. A reading written from the brief alone carries whatever the brief already lost, which is
// the fault this stands in front of.
func SayWhatYouUnderstood(one *Job) string {
	said := []string{ServesAPerson(one.Product)}
	if asked := AskedInTheseWords(one.Request, one.Brief); asked != "" {
		said = append(said, asked)
	}
	said = append(said, one.Brief, theShapeOfAnUnderstanding())
	return strings.Join(said, "\n\n")
}

// TheUnderstandingAsk is the phrase every ask for a reading carries, and the phrase a double answers
// a reading to. It is a constant for the reason the outcome marker is one: the ask and everything
// that recognises the ask must not drift apart.
const TheUnderstandingAsk = "write no plan yet"

// UnderstandingMarker opens the first line of a reading, and it is how anything holding a reply can
// tell one from a plan.
const UnderstandingMarker = "Understood:"

// theShapeOfAnUnderstanding is what the system asks for, in the shape it reads back.
func theShapeOfAnUnderstanding() string {
	return fmt.Sprintf("Do no work yet, and %s. Read the repository, the brief and the "+
		"sentence, then say what you understand this job to be. Answer in these lines and nothing else:\n\n"+
		"Understood: what this work is, in a few sentences\n"+
		"Not: what this work is not, so a wrong reading is excluded rather than left open\n"+
		"Told: something the person who declared this job stated, one line each, at most %d\n"+
		"Assumed: something you filled in yourself, one line each, at most %d\n"+
		"Unknown: something you do not know, one line each, at most %d\n"+
		"Confidence: how sure you are that you understood, in your own words\n"+
		"Question 1: something you cannot answer from the repository, the brief and the sentence\n\n"+
		"Told and Assumed are different things and today they read the same, so mark every footing as "+
		"one or the other. Ask between 1 and %d questions, numbered from 1. A question whose answer is "+
		"in the code is not a question, it is reading you have not done. Do not ask whether to go on: "+
		"this stops for a person either way. Nothing is compared against your confidence and no number "+
		"gates anything, so state it for the person rather than for the system.",
		TheUnderstandingAsk, IdeationPoints, IdeationPoints, IdeationPoints, IdeationQuestions)
}

// theSecondUnderstandingAsk is the sentence the second ask is recognised by, so a session is asked
// twice and never a third time. It is a constant because the ask and the reading of it must not
// drift: a bound that stops matching asks for ever, and every ask is a task somebody pays for.
const theSecondUnderstandingAsk = "asked what you understood once already"

// AskedWhatItUnderstoodAgain says whether a prompt is the second ask.
func AskedWhatItUnderstoodAgain(prompt string) bool {
	return strings.Contains(prompt, theSecondUnderstandingAsk)
}

// AskedForAnUnderstandingTheSystemCanRead is the second ask, where a reply carried no record the
// system could read. It carries the refusal, so the session is told what was wrong with what it sent.
func AskedForAnUnderstandingTheSystemCanRead(why string) string {
	return fmt.Sprintf("The system %s and could not read one out of your answer: %s\n\nAnswer with the "+
		"lines and nothing else. %s", theSecondUnderstandingAsk, why, theShapeOfAnUnderstanding())
}

// NoUnderstandingToAnswer is why a planned job stops when its session was asked twice and answered
// with nothing the system could read.
//
// It stops rather than planning. A job whose reading nobody could read is a job nobody has agreed
// with, and planning from it is planning the thing this gate exists to stop, after paying for two
// tasks to find out.
func NoUnderstandingToAnswer(why string) string {
	return fmt.Sprintf("this job serves a sentence, so it says what it understood before it plans, and "+
		"the session was asked twice and answered with nothing the system could read: %s. Read what it "+
		"said with krewe task list, and declare the job again with a brief that says what to read", why)
}

// AskingWhetherThisIsRight is what a person is asked, and it asks for words rather than for a word.
//
// It says there is nothing to approve here. A person shown a record and a plan gate they already know
// answers "yes" out of habit, and a "yes" here is an answer that touches no question and leaves every
// one of them unknown, which is a worse outcome than silence because it looks like agreement.
func AskingWhetherThisIsRight(sentence, understanding string) string {
	return fmt.Sprintf("This job has not started and has not planned yet. Here is what it understands "+
		"the work to be, and here is the sentence it serves.\n\nThe sentence: %s\n\n%sWhat it "+
		"understands:\n\n%s\n\nAnswer the questions in your own words, opening each answer with the "+
		"number it answers, for example \"1: ...\". What you write is kept whole and the plan is written "+
		"from it. A question you leave alone stays unknown rather than being taken as agreed, and so "+
		"does anything above marked Assumed. There is nothing to approve here: the plan comes next, and "+
		"that is the one you approve.", sentence, TheWarningsAbove(understanding), understanding)
}

// StillUnknown is the questions a person's answer did not touch.
//
// The arithmetic is the one an approved plan is already held to: the questions are numbered, an answer
// opens with the number it answers, and a number nothing carries is a question nobody answered. It
// costs no model call and anybody holding the record can work it out again.
//
// An answer that touches nothing leaves every question here, which is the point. A person who wrote
// "yes" has said nothing about the work, and the record says so rather than reading the silence as
// agreement.
func StillUnknown(understanding, answer string) []IdeationQuestion {
	one := IdeationIn(understanding)
	if len(one.Questions) == 0 {
		return nil
	}
	answered := map[int]bool{}
	for _, line := range strings.Split(answer, "\n") {
		if found := accountLine.FindStringSubmatch(line); found != nil {
			if number, err := strconv.Atoi(found[1]); err == nil {
				answered[number] = true
			}
		}
	}
	var left []IdeationQuestion
	for _, question := range one.Questions {
		if !answered[question.Number] {
			left = append(left, question)
		}
	}
	return left
}

// WhatWeUnderstand is the record and the person's answer as the session writing the plan is given
// them.
//
// The marks travel with it. What the session assumed is still an assumption after a person has
// answered, unless they answered about it, and a question nobody touched is named as still open
// rather than dropped. A plan written from a record whose assumptions had quietly become facts is the
// same fault as a plan written from a misreading.
func WhatWeUnderstand(one *Job) string {
	if one.Ideation == "" {
		return ""
	}
	said := fmt.Sprintf("This is what you said you understood before planning, and what a person then "+
		"said about it. Write the plan against both.\n\nWhat you understood:\n\n%s\n\nThe person "+
		"answered, in their own words:\n\n%s", one.Ideation, one.IdeationAnswer)
	if left := StillUnknown(one.Ideation, one.IdeationAnswer); len(left) > 0 {
		said += fmt.Sprintf("\n\nThe answer did not touch %s, so %s still unknown: %s. Plan around what "+
			"is unknown rather than deciding it quietly, and say in the plan where it bites.",
			pluralQuestions(len(left)), areOrIs(len(left)), theQuestionsLeft(left))
	}
	said += "\n\nEvery line marked Assumed is still an assumption. Carry it as one."
	return said
}

// theQuestionsLeft names the questions an answer left alone, with their numbers, so a reader can see
// which of the record's questions is still open without counting.
func theQuestionsLeft(left []IdeationQuestion) string {
	said := make([]string, 0, len(left))
	for _, one := range left {
		said = append(said, fmt.Sprintf("question %d, %s", one.Number, one.Text))
	}
	return strings.Join(said, "; ")
}

// pluralQuestions and areOrIs keep the sentence readable for one question and for several.
func pluralQuestions(count int) string {
	if count == 1 {
		return "one question"
	}
	return fmt.Sprintf("%d questions", count)
}

func areOrIs(count int) string {
	if count == 1 {
		return "it is"
	}
	return "they are"
}
