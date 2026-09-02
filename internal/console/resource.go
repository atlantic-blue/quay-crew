// Package console is the operator's full screen terminal interface, in the shape of k9s. The registry
// is the point: adding a view is declaring a Resource, never building a screen.
package console

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// State is how a row is doing, so the console can colour it without knowing what the row is.
type State int

const (
	// StateUnknown is the default: no colour, no claim.
	StateUnknown State = iota
	// StateReady is healthy and idle.
	StateReady
	// StateBusy is working right now.
	StateBusy
	// StateStopped ended cleanly.
	StateStopped
	// StateFailed ended badly and wants attention.
	StateFailed
)

// Column is one column of a resource's table. A width of zero flexes to fill what is left over,
// and at most one column should flex.
type Column struct {
	Title string
	Width int
	// Give is the order a column is dropped in when the window is too narrow: 1 goes first, zero
	// never. Without it the line is cut at the end rather than at the least useful column.
	Give int
	// Colour is the escape sequence a cell in this column is written in, from the cell's own text.
	// Nil leaves the cell in whatever the row is, which is what every column was before a listing of
	// thirty rows in one colour turned out to be a listing nobody could read.
	//
	// It is a property of the column rather than of the row because the rule is about the kind of
	// thing in it: a workspace is coloured by which workspace it is, a mode by how much it allows.
	Colour func(cell string) string
}

// Row is one listed item: the cells to show, the identifier actions operate on, and the parent it
// belongs to so a drilled down view can scope to it.
type Row struct {
	ID     string
	Parent string
	Cells  []string
	State  State
	// Label is what to call this row in the breadcrumb after drilling into it, for example a
	// workspace's name rather than its identifier. Empty falls back to the identifier.
	Label string
	// Detail is the whole of what a row is about, when the cells can only hold a summary of it. A
	// level's context is the case: the listing shows its first line and an editor needs all of it.
	Detail string
	// Address is what a person would type for this row, when that is not what they read. A workspace
	// and a project are addressed by the name in the listing, and a job by the shortened identifier
	// beside its title, so a job is the one row where the two differ. Empty falls back to the name.
	Address string
}

// Name is what to call the row where a human reads it.
func (r Row) Name() string {
	if r.Label != "" {
		return r.Label
	}
	return r.ID
}

// Typed is what a person would type to reach this row, which is what the position line is built from.
func (r Row) Typed() string {
	if r.Address != "" {
		return r.Address
	}
	return r.Name()
}

// Lister returns the current rows for a resource. Parent is empty for an unscoped view, or the
// identifier drilled down from, for example a workspace id when listing that workspace's sessions.
type Lister func(ctx context.Context, parent string) ([]Row, error)

// Summariser is one line about the whole listing, for a view whose rows do not answer the question
// on their own. It returns the line and the state it is drawn in, and an empty line draws nothing.
//
// It is a different question from the one a Lister answers: a lister says which ones are there, and
// this says what they add up to and whether that is too many.
type Summariser func(ctx context.Context, parent string) (string, State)

// Action is a key bound operation on the selected row. Exactly one of Run and Shell is set; Shell
// runs with the console suspended.
//
// Shell returns an error rather than a nil command when it cannot proceed, because "nothing to run"
// is not a reason the operator can act on.
type Action struct {
	Key string
	// Also are further keys that do the same thing. The header shows Key alone, so the hints stay one
	// line per action; the question mark lists all of them, which is where somebody goes looking for
	// the spelling they used to type.
	Also []string
	// Moved are keys this action used to answer to and does not any more. Pressing one says what to
	// press instead rather than doing nothing, because a key that quietly stopped working is how an
	// operator learns to distrust every other key on the screen. They are not listed in the help: the
	// way off a key is a refusal, not an entry beside the keys that still work.
	Moved []string
	Label string
	// Confirm makes the console ask before it acts, naming the row it is about to act on. Every
	// destructive key sets it: the list is full of conversations, and there is no way back from
	// pressing the wrong one.
	Confirm bool
	// Costs says whether this row is the case worth asking about, for a key that takes something away
	// on some rows and nothing on others. Nil asks about every row, which is what Confirm alone means.
	// Restarting is the one: a stopped thread has no turn and nothing attached to it, so there is
	// nothing to be careful about, and asking would only be in the way.
	Costs func(row Row) bool
	Run   func(ctx context.Context, row Row) error
	Shell func(row Row) (*exec.Cmd, error)
	// After runs once a Shell command has finished, for the part that cannot happen while the
	// terminal belongs to somebody else. Editing context is the case: the editor writes a file and
	// this is what tells the system about it.
	After func(ctx context.Context, row Row) error
	// Refuses says why this key cannot act on this row, and is nil where it always can. A key with it
	// says so before it opens anything: asking somebody to type an answer the system will throw away
	// is worse than the key doing nothing, because they wrote the answer first.
	Refuses func(row Row) error
	// Asks is what a key that wants a line of text says while it waits, for example "call it". A key
	// with it opens the line rather than acting, and RunTyped is what acts once enter is pressed.
	Asks string
	// EmptyMeans is what an empty line does on this key, for the hint beside it, and is empty where an
	// empty line is refused. Naming a session is the one: an empty name clears the name, and nobody
	// would guess that. An answer to a job is the other: an empty one is refused, and a hint offering
	// to clear something would be a lie about the next keystroke.
	EmptyMeans string
	// RunTyped acts on the row with the line that was typed. Empty text is a real answer, not a
	// cancel: it is how a name is cleared. Escape is how nothing happens.
	RunTyped func(ctx context.Context, row Row, typed string) error
	// Typed is what the line starts as, so editing a name begins from the name rather than from
	// nothing and the operator does not retype it to change one word.
	Typed func(row Row) string
	// Offers are the things this key asks the operator to pick between, in the order they are offered.
	// A key with them opens a picker rather than acting, and RunChosen is what acts once one is
	// picked. Confirm still applies, and Widens decides when: a key that both narrows and widens asks
	// only when the pick gives the row more than it had.
	Offers []string
	// RunChosen acts on the row with what was picked. It is the Run of a key that offers.
	RunChosen func(ctx context.Context, row Row, chosen string) error
	// Widens says whether picking this would give the row more than it has now, which is what decides
	// whether a pick is asked about. Nil means every pick is treated the same way Confirm says.
	Widens func(row Row, chosen string) bool
	// OnScope says this key acts on what the view is scoped to rather than on the row under the
	// cursor, and the row it is handed then carries that scope as its identifier. The running work is
	// the case: it lists the tasks of one session, and shelling into that session has to work on the
	// job that has produced no task yet, where there is no row to stand on at all.
	OnScope bool
	// Conversation says this key opens the row's conversation. Where the console already has one
	// beside it, which is what a panel is, that is where it opens: the listing stays on screen and
	// the conversation the operator is talking to becomes the one they pointed at. A console with
	// nothing beside it has only its own screen to give, and Shell is what it hands over.
	Conversation bool
	// Descend opens another resource scoped to the selected row, the way enter does where a view has
	// somewhere to drill into. It exists because a session already spends enter on opening the
	// conversation, which is the thing an operator does most, and a history is worth a key of its own
	// rather than taking the cheapest one away from what it is used for.
	Descend string
}

// Bound says whether a keypress runs this action.
func (a Action) Bound(key string) bool {
	if a.Key == key {
		return true
	}
	for _, also := range a.Also {
		if also == key {
			return true
		}
	}
	return false
}

// Keys is every key this action answers to, primary first, for the help list.
func (a Action) Keys() []string {
	return append([]string{a.Key}, a.Also...)
}

// WasBound says whether key is one this action used to answer to, which is what a refusal naming the
// new spelling is built from.
func (a Action) WasBound(key string) bool {
	for _, moved := range a.Moved {
		if moved == key {
			return true
		}
	}
	return false
}

// Resource is one thing the console can list. Adding a view is adding one of these.
type Resource struct {
	// Name is the canonical name, typed as ":sessions" and shown in the breadcrumb.
	Name string
	// Aliases are shorter spellings the command bar also accepts.
	Aliases []string
	Columns []Column
	List    Lister
	// Summary is the line drawn above the columns, and nil in a view with nothing to add up. The room
	// view is the one so far: eighteen rows of megabytes never said what the machine had left.
	Summary Summariser
	Actions []Action
	// DrillTo is the resource enter descends into, scoped to the selected row. Empty means enter
	// does nothing here.
	DrillTo string
	// DrillBy is the identifier the child is scoped by, when it is not the row's own. Jobs is the
	// case: what a job did is its session's tasks, so the child is scoped by the session the row
	// names rather than by the job. Nil scopes by the row's own identifier, which is every other
	// view. The error is what the operator is told when there is nothing to descend to yet.
	DrillBy func(row Row) (string, error)
	// SortBy is the column the console orders rows by, and marks with an arrow so the order is
	// visible. Rows that tie keep the order the control plane returned them in.
	SortBy int
}

// One is what to call a single row of this resource, for a sentence about one of them: "stop session
// d754610f?". Every resource here is named as a plural, so trimming it is enough and beats carrying a
// second name on every declaration.
func (r Resource) One() string {
	return strings.TrimSuffix(r.Name, "s")
}

// Registry holds the resources the console knows about and resolves what the operator types.
type Registry struct {
	byName  map[string]Resource
	byToken map[string]string
	order   []string
}

// NewRegistry indexes resources by name and alias. It rejects a duplicate name or alias rather than
// letting one resource silently shadow another, because the shadowed view would simply never open.
func NewRegistry(resources ...Resource) (*Registry, error) {
	registry := &Registry{
		byName:  make(map[string]Resource, len(resources)),
		byToken: make(map[string]string, len(resources)*2),
	}
	for _, resource := range resources {
		if err := registry.add(resource); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Add registers a resource after the registry exists, which the keys view needs because it reads the
// registry it lives in.
func (r *Registry) Add(resource Resource) error {
	return r.add(resource)
}

func (r *Registry) add(resource Resource) error {
	if resource.Name == "" {
		return fmt.Errorf("console: resource has no name")
	}
	if resource.List == nil {
		return fmt.Errorf("console: resource %q has no lister", resource.Name)
	}
	if _, taken := r.byName[resource.Name]; taken {
		return fmt.Errorf("console: resource %q is registered twice", resource.Name)
	}
	r.byName[resource.Name] = resource
	r.order = append(r.order, resource.Name)

	for _, token := range append([]string{resource.Name}, resource.Aliases...) {
		if owner, taken := r.byToken[token]; taken {
			return fmt.Errorf("console: %q already resolves to resource %q", token, owner)
		}
		r.byToken[token] = resource.Name
	}
	return nil
}

// Resolve maps what was typed into the command bar to a resource. Matching is case insensitive and
// ignores surrounding space and a leading colon, so ":Sessions" and "s" both land.
func (r *Registry) Resolve(token string) (Resource, bool) {
	cleaned := cleanToken(token)
	if cleaned == "" {
		return Resource{}, false
	}
	name, known := r.byToken[cleaned]
	if !known {
		return Resource{}, false
	}
	resource, found := r.byName[name]
	return resource, found
}

// cleanToken is what a typed word means as a view name. Case and surrounding space do not matter,
// and a leading colon is how the bar was opened rather than part of the word.
func cleanToken(typed string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(typed), ":")))
}

// Get returns a resource by its canonical name.
func (r *Registry) Get(name string) (Resource, bool) {
	resource, found := r.byName[name]
	return resource, found
}

// Names returns every registered resource name in registration order, for the help view.
func (r *Registry) Names() []string {
	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
}

// Offer returns the views a typed prefix could mean, by canonical name, in registration order. An
// empty prefix offers all of them, which is what an operator who has just pressed colon needs: the
// command bar asks a question and until now gave nothing to answer it with.
func (r *Registry) Offer(typed string) []string {
	cleaned := cleanToken(typed)
	offered := make([]string, 0, len(r.order))
	for _, name := range r.order {
		if matchesPrefix(r.byName[name], cleaned) {
			offered = append(offered, name)
		}
	}
	return offered
}

// matchesPrefix says whether any spelling of a resource starts with what has been typed.
func matchesPrefix(resource Resource, prefix string) bool {
	if prefix == "" {
		return true
	}
	for _, token := range append([]string{resource.Name}, resource.Aliases...) {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

// Spellings returns a resource's name and its aliases, for a list that has to say what to type.
func (r *Registry) Spellings(name string) []string {
	resource, found := r.byName[name]
	if !found {
		return nil
	}
	return append([]string{resource.Name}, resource.Aliases...)
}

// Tokens returns every name and alias, sorted, for command bar completion.
func (r *Registry) Tokens() []string {
	tokens := make([]string, 0, len(r.byToken))
	for token := range r.byToken {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return tokens
}
