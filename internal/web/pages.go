package web

import (
	"context"
	"net/http"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/session"
)

// shell is what every page carries: what the tab says, and where the operator is.
type shell struct {
	Title string
	Where string
}

// sessionRow is one conversation in the listing.
type sessionRow struct {
	ID      string
	Short   string
	Address string
	Name    string
	Status  string
	Age     string
}

type sessionsPage struct {
	shell
	Sessions []sessionRow
}

// taskRow is one exchange. A task that failed carries its failure instead of a reply, and says so,
// because a blank reply and a refused task must never read the same. A task still running has no
// reply yet, for the same reason: an empty box reads as a task that answered nothing.
type taskRow struct {
	When    string
	Prompt  string
	Reply   string
	Running bool
	Failed  bool
	Failure string
}

type sessionPage struct {
	shell
	Session sessionRow
	Tasks   []taskRow
}

func (v *view) sessions(w http.ResponseWriter, r *http.Request) {
	// Presence, so this page says the same thing the console and the command line say about a session
	// holding a live conversation. It costs a question to each idle session's sandbox.
	listed, err := v.reader.ListSessions(r.Context(), &quaycrewv1.ListSessionsRequest{Presence: true})
	if err != nil {
		http.Error(w, "the crew did not answer: "+err.Error(), http.StatusBadGateway)
		return
	}
	names, err := v.names(r.Context())
	if err != nil {
		http.Error(w, "the crew did not answer: "+err.Error(), http.StatusBadGateway)
		return
	}

	// The order the crew answered in, untouched. It is last moved first, so the age column reads in
	// order, and it is the same order the console and the command line show: a page that sorted the
	// listing again would be a second order to keep in step with this one.
	rows := make([]sessionRow, 0, len(listed.GetSessions()))
	for _, session := range listed.GetSessions() {
		rows = append(rows, row(session, names))
	}

	v.render(w, "sessions.html", sessionsPage{
		shell:    shell{Title: "sessions", Where: "every live conversation"},
		Sessions: rows,
	})
}

func (v *view) session(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	got, err := v.reader.GetSession(r.Context(), &quaycrewv1.GetSessionRequest{Id: id})
	if err != nil || got.GetSession() == nil {
		http.Error(w, "no session here", http.StatusNotFound)
		return
	}
	names, err := v.names(r.Context())
	if err != nil {
		http.Error(w, "the crew did not answer: "+err.Error(), http.StatusBadGateway)
		return
	}
	turns, err := v.reader.ListTasks(r.Context(), &quaycrewv1.ListTasksRequest{Session: got.GetSession().GetId()})
	if err != nil {
		http.Error(w, "the crew did not answer: "+err.Error(), http.StatusBadGateway)
		return
	}

	tasks := make([]taskRow, 0, len(turns.GetTasks()))
	for _, turn := range turns.GetTasks() {
		tasks = append(tasks, task(turn))
	}
	head := row(got.GetSession(), names)

	v.render(w, "session.html", sessionPage{
		shell:   shell{Title: head.Name, Where: head.Address},
		Session: head,
		Tasks:   tasks,
	})
}

func task(turn *quaycrewv1.Task) taskRow {
	return taskRow{
		When:    turn.GetOccurredAt().AsTime().Local().Format("15:04:05"),
		Prompt:  turn.GetPrompt(),
		Reply:   turn.GetReply(),
		Running: turn.GetStatus() == "running",
		Failed:  turn.GetStatus() == "failed",
		Failure: turn.GetFailure(),
	}
}

func row(one *quaycrewv1.Session, names map[string]string) sessionRow {
	return sessionRow{
		ID:      one.GetId(),
		Short:   display.ShortID(one.GetId()),
		Address: address(one, names),
		Name:    display.SessionName(one),
		Status:  display.StatusLabel(one),
		Age:     display.Age(session.LastMoved(one)),
	}
}

// address is the session written the way the operator says it, workspace/project/session. It falls
// back to the identifier for either level, so a session whose workspace was renamed under it still
// reads as something rather than as a gap.
func address(session *quaycrewv1.Session, names map[string]string) string {
	parts := []string{
		display.Name(names[session.GetWorkspace()], session.GetWorkspace()),
		display.Name(names[session.GetProject()], session.GetProject()),
		display.ShortID(session.GetHandle()),
	}
	return strings.Join(parts, "/")
}

// names is every workspace and project identifier against what it is called, so a listing reads as
// places rather than as identifiers. One map for both, because the identifiers do not collide and
// the caller only ever asks "what is this one called".
func (v *view) names(ctx context.Context) (map[string]string, error) {
	named := map[string]string{}
	workspaces, err := v.reader.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
	if err != nil {
		return nil, err
	}
	for _, workspace := range workspaces.GetWorkspaces() {
		named[workspace.GetId()] = workspace.GetName()
	}
	projects, err := v.reader.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
	if err != nil {
		return nil, err
	}
	for _, project := range projects.GetProjects() {
		named[project.GetId()] = project.GetName()
	}
	return named, nil
}
