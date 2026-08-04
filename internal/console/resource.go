// Package console is the operator's full screen terminal interface. It is a registry of resources,
// one listed at a time, in the shape of k9s: a command bar switches views, enter drills down, escape
// goes back, and the footer says which keys do what here.
//
// The registry is the point. Every resource the crew has should be inspectable from the same place,
// so adding a view is declaring a Resource, never building a screen.
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
}

// Name is what to call the row where a human reads it.
func (r Row) Name() string {
	if r.Label != "" {
		return r.Label
	}
	return r.ID
}

// Lister returns the current rows for a resource. Parent is empty for an unscoped view, or the
// identifier drilled down from, for example a workspace id when listing that workspace's sessions.
type Lister func(ctx context.Context, parent string) ([]Row, error)

// Action is a key bound operation on the selected row.
//
// Exactly one of Run and Shell is set. Run performs the action and returns. Shell returns a command
// to run with the console suspended, which is how shelling into a session's container works.
//
// Shell returns an error rather than a nil command when it cannot proceed, so the operator is told
// why. "Nothing to run" is not a reason, and the reasons here are ones they can act on: a session
// with no conversation yet, or one that has been stopped.
type Action struct {
	Key string
	// Also are further keys that do the same thing. The header shows Key alone, so the hints stay one
	// line per action; the question mark lists all of them, which is where somebody goes looking for
	// the spelling they used to type.
	Also  []string
	Label string
	// Confirm makes the console ask before it acts, naming the row it is about to act on. Every
	// destructive key sets it: the list is full of conversations, and there is no way back from
	// pressing the wrong one.
	Confirm bool
	Run     func(ctx context.Context, row Row) error
	Shell   func(row Row) (*exec.Cmd, error)
	// After runs once a Shell command has finished, for the part that cannot happen while the
	// terminal belongs to somebody else. Editing context is the case: the editor writes a file and
	// this is what tells the crew about it.
	After func(ctx context.Context, row Row) error
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

// Resource is one thing the console can list. Adding a view is adding one of these.
type Resource struct {
	// Name is the canonical name, typed as ":sessions" and shown in the breadcrumb.
	Name string
	// Aliases are shorter spellings the command bar also accepts.
	Aliases []string
	Columns []Column
	List    Lister
	Actions []Action
	// DrillTo is the resource enter descends into, scoped to the selected row. Empty means enter
	// does nothing here.
	DrillTo string
	// SortBy is the column the console orders rows by, and marks with an arrow so the order is
	// visible. Rows that tie keep the order the control plane returned them in.
	SortBy int
}

// One is what to call a single row of this resource, for a sentence about one of them: "stop thread
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
	cleaned := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), ":")))
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
	cleaned := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(typed), ":")))
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
