package job

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A job says what it would build, as verticals a person accepts, before it writes a plan.
//
// Ideation put the reading in front of a person and the plan gate put the steps in front of them.
// Between the two there was nothing. A job went from an understanding somebody agreed with straight
// to seven steps, and the steps say what the crew will do rather than what a person will get, so the
// one question nobody was ever asked is which deliverable arrives first. A plan of seven steps that
// all land together is a plan with one delivery at the end of it, and that is the shape every run
// that went wrong here had.
//
// So the job lists the verticals it would build. Each one names what a person can do when it lands
// and what they are shown. A person accepts the list, and nothing is planned until they do.
//
// A database is not a deliverable, and nor is a piece of infrastructure. Those are required work
// towards a deliverable, so a list of a schema, a queue and a role is one vertical with its plumbing
// inside it rather than three, and this file refuses that list rather than putting it to a person.
// The rule is here rather than in the wording of the ask, because an ask is advice and a rule is a
// rule: the same instruction lived in the plan's ask for weeks and the plans kept arriving as
// layers.
const (
	// DesignVerticals is how many verticals one list may carry.
	//
	// The plan's ceiling, for the plan's reason: a person reads this in a terminal instead of reading
	// the result, so a list as long as the work costs the reading and buys nothing. Chosen rather than
	// measured. What replaces it is the distribution of verticals a person accepts, which the record
	// now holds: after fifty accepted lists, the ninety fifth percentile is the number.
	DesignVerticals = 7
	// DesignLineLimit is how long one line may be. It is the title's ceiling, because both are one
	// line a person reads.
	DesignLineLimit = TitleLimit
	// DesignLimit is how long the whole record may be once the system renders it. The record is put to
	// a person as one question, and a question has its own ceiling in asking.go, so the record plus the
	// lines around it has to fit inside QuestionLimit.
	DesignLimit = 3000
)

// designLine is the shape the system asks for and reads back: a vertical, a vertical that came from
// what the person said, and the thing a person is shown when it lands.
//
// Read off the reply rather than reported, the way a plan and a reading already are. What it finds is
// then what the session meant to say, rather than a sentence that happened to hold the word.
var designLine = regexp.MustCompile(
	`(?im)^[ \t]*(vertical|yours|shown)[ \t]+(\d+)[ \t]*[:.][ \t]*(.+?)[ \t]*$`)

// plumbingWord is the work a system does for itself. None of these is a thing a person can be shown
// working, and each of them is real work that some vertical needs.
//
// The list is deliberately short and concrete. A long list guesses at what somebody might write and
// refuses work that was fine, and a refusal here costs a task and a person's patience.
var plumbingWord = regexp.MustCompile(`(?i)\b(databases?|schemas?|migrations?|tables?|columns?|` +
	`indexes|indices|queues?|topics?|caches?|buckets?|pipelines?|infrastructure|terraform|` +
	`containers?|clusters?|daemons?|endpoints?|protobuf|proto|grpc|libraries|library|modules?|` +
	`packages?|interfaces?|backends?|brokers?|lambdas?|kubernetes|docker|deployments?|storage|` +
	`stores?|middleware|scaffolding|boilerplate|abstractions?|wiring|plumbing|roles?|` +
	`repositories|repository|secrets?|credentials?|webhooks?)\b`)

// aPerson is somebody who can be shown the thing working. The rule below turns on this and nothing
// else: a line about infrastructure that names the person it serves is a vertical, and the same line
// with the person missing is the plumbing on its own.
var aPerson = regexp.MustCompile(
	`(?i)\b(a person|the person|people|persons|operators?|somebody|someone|anybody|anyone|` +
		`you|your|users?|customers?|developers?|whoever)\b`)

// Vertical is one thing the job would build: what a person can do when it lands, what they are
// shown, and whether the person or the machine put it on the list.
type Vertical struct {
	Number int
	Text   string
	Shown  string
	// Yours says this one came from what the person wrote when they sent the list back. It is the mark
	// ideation already makes between what a session was told and what it filled in for itself, on the
	// list rather than on the reading: a list a person changed and a list the machine proposed read the
	// same once they are both on the row, and they are not the same thing.
	Yours bool
}

// Design is the list of verticals a job would build.
type Design struct {
	Verticals []Vertical
}

// ReadDesign is the list a reply carries, and the refusal where it carries none the system can put to
// a person.
func ReadDesign(reply string) (Design, error) {
	held := map[int]*Vertical{}
	var order []int
	for _, found := range designLine.FindAllStringSubmatch(reply, -1) {
		number, err := strconv.Atoi(found[2])
		if err != nil {
			return Design{}, fmt.Errorf("%q is not numbered with a number", found[0])
		}
		said := TidySentence(found[3])
		if said == "" {
			continue
		}
		one, seen := held[number]
		if !seen {
			one = &Vertical{Number: number}
			held[number], order = one, append(order, number)
		}
		switch strings.ToLower(found[1]) {
		case "shown":
			if one.Shown == "" {
				one.Shown = said
			}
		default:
			// The first line under a number stands, the way the first thing said under a heading stands in
			// a reading: a reply that lists the same vertical twice changed its mind in the middle, and the
			// system reads the first rather than choosing between them.
			if one.Text == "" {
				one.Text, one.Yours = said, strings.EqualFold(found[1], "yours")
			}
		}
	}
	one := Design{}
	for _, number := range order {
		one.Verticals = append(one.Verticals, *held[number])
	}
	sort.SliceStable(one.Verticals, func(i, j int) bool {
		return one.Verticals[i].Number < one.Verticals[j].Number
	})
	if err := one.readable(); err != nil {
		return Design{}, err
	}
	return one, nil
}

// readable is every rule a list is held to, in one place, and the refusal that teaches the shape.
func (d Design) readable() error {
	if len(d.Verticals) == 0 {
		return fmt.Errorf("this reply carries no list the system can read: write one line per vertical, "+
			"each opening with %s, and a line under it opening with %s saying what a person is shown",
			`Vertical 1:`, `Shown 1:`)
	}
	if len(d.Verticals) > DesignVerticals {
		return fmt.Errorf("this list has %d verticals and it may have %d: a person reads this instead of "+
			"reading the result, so say the %d that land",
			len(d.Verticals), DesignVerticals, DesignVerticals)
	}
	for i, one := range d.Verticals {
		if one.Number != i+1 {
			return fmt.Errorf("this list is numbered %s: number the verticals from 1 upwards with none "+
				"missing and none repeated, because a person answers about them by number", numbersOf(d))
		}
		if one.Text == "" {
			return fmt.Errorf("vertical %d says nothing: say what a person can do when it lands", one.Number)
		}
		if one.Shown == "" {
			return fmt.Errorf("vertical %d says what it is and never what a person is shown: write a line "+
				"opening with \"Shown %d:\". A vertical nobody can be shown is not a vertical",
				one.Number, one.Number)
		}
		if len(one.Text) > DesignLineLimit {
			return fmt.Errorf("vertical %d is %d bytes and it may be %d: it is one line a person reads",
				one.Number, len(one.Text), DesignLineLimit)
		}
		if len(one.Shown) > DesignLineLimit {
			return fmt.Errorf("what vertical %d shows is %d bytes and it may be %d: it is one line a "+
				"person reads", one.Number, len(one.Shown), DesignLineLimit)
		}
	}
	if err := d.deliverable(); err != nil {
		return err
	}
	if kept := DesignText(d); len(kept) > DesignLimit {
		return fmt.Errorf("this list is %d bytes and it may be %d: it is put to a person as one question, "+
			"so say less in each line rather than more", len(kept), DesignLimit)
	}
	return nil
}

// deliverable is the rule that decides whether a list is a list.
//
// A vertical is only a vertical if a person can be shown the thing working. A database is not a
// deliverable and nor is a piece of infrastructure: those are required work towards one, so a list
// made of them is one vertical with its plumbing inside it, and the number of lines it carries says
// nothing about how many things a person gets.
//
// The measurement is two vocabularies and no model call, which is the argument the drift measure in
// request.go already makes: it costs nothing, it works with any backend, and anybody holding the
// record can work out again why a line was refused. A line that names infrastructure and names
// nobody it serves is plumbing. The same line with the person in it is a vertical, because then it
// says who is shown the thing working.
func (d Design) deliverable() error {
	var plumbing []Vertical
	var words []string
	for _, one := range d.Verticals {
		if word := OnlyPlumbing(one.Text + " " + one.Shown); word != "" {
			plumbing = append(plumbing, one)
			words = append(words, word)
		}
	}
	if len(plumbing) == 0 {
		return nil
	}
	if len(plumbing) == len(d.Verticals) && len(d.Verticals) > 1 {
		return fmt.Errorf("this is not %d verticals, it is one vertical with its plumbing inside it: "+
			"%s. A database is not a deliverable and nor is a piece of infrastructure, they are required "+
			"work towards one. Say the thing a person can be shown working, and name the person in the "+
			"line", len(d.Verticals), theyNamed(plumbing, words))
	}
	return fmt.Errorf("%s. That is required work towards a deliverable rather than a deliverable: a "+
		"vertical is only a vertical if a person can be shown it working, so fold it into the vertical "+
		"it serves, and name the person in the line", theyNamed(plumbing, words))
}

// theyNamed is the lines a refusal has to show, with the word each one was refused for, so a session
// can see which line to rewrite rather than guessing at the whole list.
func theyNamed(plumbing []Vertical, words []string) string {
	said := make([]string, 0, len(plumbing))
	for i, one := range plumbing {
		said = append(said, fmt.Sprintf("vertical %d names a %s and names nobody it serves, %q",
			one.Number, words[i], one.Text))
	}
	return strings.Join(said, "; ")
}

// OnlyPlumbing is the infrastructure word a line names where it names nobody at all, and empty where
// the line is something a person can be shown.
func OnlyPlumbing(line string) string {
	if NamesAPerson(line) {
		return ""
	}
	return strings.ToLower(plumbingWord.FindString(line))
}

// NamesAPerson says whether a line names somebody who can be shown the thing working. It is the
// whole of the rule above: the words themselves decide nothing, and who the line serves decides
// everything.
func NamesAPerson(line string) bool { return aPerson.MatchString(line) }

// numbersOf is the numbering a list carried, for a refusal that has to show it.
func numbersOf(d Design) string {
	said := make([]string, 0, len(d.Verticals))
	for _, one := range d.Verticals {
		said = append(said, strconv.Itoa(one.Number))
	}
	return strings.Join(said, ", ")
}

// DesignText is the list as the system keeps it, in the shape it reads back.
//
// The system's own rendering rather than the reply, for the reason a plan and a reading are kept that
// way: what a person accepts and what the session writing the plan is later handed are then the same
// lines, and the reasoning a model wraps around its answer is what makes a record as expensive to
// read as the work.
func DesignText(d Design) string {
	lines := make([]string, 0, len(d.Verticals)*2)
	for _, one := range d.Verticals {
		opening := "Vertical"
		if one.Yours {
			opening = "Yours"
		}
		lines = append(lines, fmt.Sprintf("%s %d: %s", opening, one.Number, one.Text),
			fmt.Sprintf("Shown %d: %s", one.Number, one.Shown))
	}
	return strings.Join(lines, "\n")
}

// DesignIn is the list a kept row holds. A list the system wrote always reads back, so one that does
// not is empty rather than an error: nothing downstream of an acceptance can act on a refusal.
func DesignIn(kept string) Design {
	one, err := ReadDesign(kept)
	if err != nil {
		return Design{}
	}
	return one
}

// Designed says whether a person accepted the list this job would build.
//
// There is a flag beside the list, which is where this differs from the reading in ideation and where
// it is the same as the plan. An acceptance is one word, so a list a person sent back carries the
// same text as a list nobody has answered, and only the flag tells the two apart.
func Designed(one *Job) bool { return one != nil && one.DesignAccepted }

// pastItsDesign says whether this job is past the design stage, whether or not it went through it.
//
// A row written before this existed carries a plan and no list, and a gate that read those as owing
// one would drag work a person had already agreed to back to the beginning. So a job that holds a
// plan is past this, the way ideation already treats a job that holds an approved plan.
func pastItsDesign(one *Job) bool {
	return one != nil && (one.DesignAccepted || one.Plan != "" || one.PlanApproved)
}

// WaitingForItsDesign says whether this job still owes a person the list it would build.
//
// The same gate the plan and the reading use, held by the same person: the sentence is the trigger, a
// job under another is one part of a plan somebody already approved, and an errand has nothing to
// build a list against. It stands behind ideation, because a list of verticals written before
// anybody agreed with the reading is a list of the wrong things.
func WaitingForItsDesign(one *Job) bool {
	return Planned(one) && Ideated(one) && !pastItsDesign(one)
}

// TheDesignAsk is the phrase every ask for a list carries, and the phrase a double answers a list to.
// It is a constant for the reason the outcome marker is one: the ask and everything that recognises
// the ask must not drift apart.
const TheDesignAsk = "list the verticals you would build"

// DesignMarker is the line every list carries, and it is how anything holding a reply can tell a list
// from a plan or a reading.
//
// It is the shown line rather than the first line, because the first line opens with Vertical on a
// list the crew proposed and with Yours on a vertical the person put there. Both are lists, so a
// marker that matched only one would read the second as prose.
const DesignMarker = "Shown 1:"

// WhatWouldYouBuild is the task a job is given once a person has answered what it understood. It asks
// for the list and for no plan and no work.
//
// The sentence goes first and the agreed understanding travels with it. A list written from the brief
// alone carries whatever the brief already lost, which is the fault the stage in front of this one
// exists to catch, and repeating it here would waste the answer a person wrote.
func WhatWouldYouBuild(one *Job) string {
	said := []string{ServesAPerson(one.Product)}
	if asked := AskedInTheseWords(one.Request, one.Brief); asked != "" {
		said = append(said, asked)
	}
	said = append(said, one.Brief)
	if understood := WhatWeUnderstand(one); understood != "" {
		said = append(said, understood)
	}
	return strings.Join(append(said, theShapeOfADesign()), "\n\n")
}

// WriteTheListAgain is what a session is given when a person did not accept its list.
//
// It carries the list that was refused and what the person said, so the second list is written from
// the correction rather than from nothing. The person who said what is wrong writes no list: saying
// it is the whole of what they owe, which is the rule the plan gate already keeps.
func WriteTheListAgain(one *Job) string {
	said := fmt.Sprintf("The list you proposed was not accepted.\n\nYou proposed:\n\n%s\n\nThe person "+
		"said: %s\n\nWrite the list again from what they said, and answer with it. Do no work yet.",
		one.Design, one.Told)
	if understood := WhatWeUnderstand(one); understood != "" {
		said += "\n\n" + understood
	}
	return said + "\n\n" + theShapeOfADesign()
}

// theShapeOfADesign is what the system asks for, in the shape it reads back.
func theShapeOfADesign() string {
	return fmt.Sprintf("Do no work yet, and write no plan. Turn what a person agreed with into the "+
		"things you would build, and %s. Answer in these lines and nothing else:\n\n"+
		"Vertical 1: what a person can do when this one lands\n"+
		"Shown 1: what that person is shown when it lands, in one line\n\n"+
		"List at most %d, numbered from 1, each line under %d bytes. A vertical is only a vertical if a "+
		"person can be shown it working, so name the person in the line. A database is not a "+
		"deliverable and nor is a piece of infrastructure: those are required work towards one, so a "+
		"schema, a queue and a role are one vertical with its plumbing inside them rather than three, "+
		"and the system refuses a list of them. Where a vertical comes from what the person said, open "+
		"its line with \"Yours 2:\" instead of \"Vertical 2:\", so what they changed stays apart from "+
		"what you proposed.", TheDesignAsk, DesignVerticals, DesignLineLimit)
}

// theSecondDesignAsk is the sentence the second ask is recognised by, so a session is asked twice and
// never a third time. It is a constant because the ask and the reading of it must not drift: a bound
// that stops matching asks for ever, and every ask is a task somebody pays for.
const theSecondDesignAsk = "asked for the list once already"

// AskedForTheListAgain says whether a prompt is the second ask.
func AskedForTheListAgain(prompt string) bool {
	return strings.Contains(prompt, theSecondDesignAsk)
}

// AskedForAListTheSystemCanRead is the second ask, where a reply carried no list the system could
// read. It carries the refusal, so the session is told what was wrong with what it sent.
func AskedForAListTheSystemCanRead(why string) string {
	return fmt.Sprintf("The system %s and could not read one out of your answer: %s\n\nAnswer with the "+
		"list and nothing else. %s", theSecondDesignAsk, why, theShapeOfADesign())
}

// NoListToAccept is why a job stops when its session was asked twice and answered with no list a
// person could accept.
//
// It stops rather than planning. A job whose list nobody could read is a job nobody agreed a shape
// with, and planning from it is planning the thing this gate exists to stop, after paying for two
// tasks to find out.
func NoListToAccept(why string) string {
	return fmt.Sprintf("this job serves a sentence, so it says what it would build before it plans, and "+
		"the session was asked twice and answered with no list the system could read: %s. Read what it "+
		"said with krewe task list, and declare the job again with a brief that says what to build", why)
}

// AskingWhetherThisIsTheList is the one question the design stage puts to a person.
//
// It names the sentence and the list, and nothing else. What is being accepted is whether these
// things, in this order, get that sentence.
func AskingWhetherThisIsTheList(sentence, design string) string {
	return fmt.Sprintf("This job has not started and has no plan yet. Here is what it would build, one "+
		"line per thing you can be shown working, and here is the sentence it serves.\n\nThe sentence: "+
		"%s\n\nWhat it would build:\n\n%s\n\nDoes this list get that sentence? Answer %s and the job "+
		"plans against this list. Answer with what is wrong instead, and the crew writes the list again "+
		"from what you said: you do not have to write it yourself.",
		sentence, design, theAnswerThatApproves)
}

// AcceptsTheList says whether an answer is the acceptance.
//
// The plan's word, and deliberately the same one. Two gates a person answers, each with its own word
// for yes, is a system that teaches somebody two habits and then punishes the wrong one.
func AcceptsTheList(answer string) bool {
	return strings.EqualFold(TidySentence(answer), theAnswerThatApproves)
}

// WhatWeWouldBuild is the accepted list as the session writing the plan is given it.
//
// The marks travel with it. A vertical the person put on the list stays theirs after they accepted
// it, so a plan that quietly dropped one is a plan that dropped the thing they asked for, and a
// reader of the plan can see which of the two wrote each line.
func WhatWeWouldBuild(one *Job) string {
	if one.Design == "" || !one.DesignAccepted {
		return ""
	}
	said := fmt.Sprintf("A person accepted this list of what you would build. Plan against it, in this "+
		"order, and deliver each one so it can be shown working on its own.\n\n%s", one.Design)
	if yours := DesignIn(one.Design); yoursIn(yours) > 0 {
		said += "\n\nA line opening with Yours is one the person put on the list themselves. Carry it as " +
			"theirs."
	}
	return said
}

// yoursIn is how many verticals of a list came from the person.
func yoursIn(d Design) int {
	count := 0
	for _, one := range d.Verticals {
		if one.Yours {
			count++
		}
	}
	return count
}
