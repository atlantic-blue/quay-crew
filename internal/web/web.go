// Package web serves a read only view of the crew to a browser on the operator's own machine.
//
// A terminal pane is a poor place to read a long reply with code in it, and that is the one gap this
// answers. It opens no new network, it needs no new call in the control plane, and it cannot change
// anything: the interface it holds names only calls that read.
package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"google.golang.org/grpc"
)

// DefaultAddress is where the view listens. The port is 8080 because it is the one everybody tries
// first, and the host is loopback because this serves one operator on one machine.
const DefaultAddress = "127.0.0.1:8080"

//go:embed templates
var templateFiles embed.FS

//go:embed static
var staticFiles embed.FS

// Reader is the whole of the control plane this view may use, and every call on it reads.
//
// The narrowing is what makes read only structural rather than promised. A handler in this package
// cannot dispatch a task, delete a workspace or stop a session, because there is no method here to
// call. TestTheViewCanOnlyRead holds that line as the interface grows.
type Reader interface {
	ListWorkspaces(context.Context, *quaycrewv1.ListWorkspacesRequest, ...grpc.CallOption) (*quaycrewv1.ListWorkspacesResponse, error)
	ListProjects(context.Context, *quaycrewv1.ListProjectsRequest, ...grpc.CallOption) (*quaycrewv1.ListProjectsResponse, error)
	ListSessions(context.Context, *quaycrewv1.ListSessionsRequest, ...grpc.CallOption) (*quaycrewv1.ListSessionsResponse, error)
	GetSession(context.Context, *quaycrewv1.GetSessionRequest, ...grpc.CallOption) (*quaycrewv1.GetSessionResponse, error)
	ListTasks(context.Context, *quaycrewv1.ListTasksRequest, ...grpc.CallOption) (*quaycrewv1.ListTasksResponse, error)
}

// Serve runs the view until ctx is done, writing the address it came up on to out.
func Serve(ctx context.Context, reader Reader, addr string, out io.Writer) error {
	if err := loopbackOnly(addr); err != nil {
		return err
	}
	handler, err := Handler(reader)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	fmt.Fprintf(out, "quay web is on http://%s\n", listener.Addr())

	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// loopbackOnly refuses to bind anywhere but this machine.
//
// The control plane listens on a local only port guarded by one shared token, and this server holds
// that token. Serving it to a routable address would hand the whole crew to the network without the
// three things that would need: a token for each device, a way to withdraw one, and a rule about
// encryption. None of those exist, so this is a wall rather than a default. An address with no host
// is refused too, because ":8080" binds every interface there is.
func loopbackOnly(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%q is not a host and a port: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("quay web serves this machine only, and %q is not on it: use %s. "+
		"Reaching the crew from another device is a separate decision, and a chat channel is the "+
		"road planned for it", addr, DefaultAddress)
}

// Handler builds the routes. It parses the templates once, so a template that does not compile is a
// failure to start rather than a broken page found later by whoever opened it.
func Handler(reader Reader) (http.Handler, error) {
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}
	view := &view{reader: reader, pages: pages}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(staticFiles))
	mux.HandleFunc("GET /{$}", view.sessions)
	mux.HandleFunc("GET /session/{id}", view.session)
	return mux, nil
}

// parsePages builds one template set per page, because each page defines a body block of its own and
// two of them in one set would collide.
func parsePages() (map[string]*template.Template, error) {
	pages := map[string]*template.Template{}
	for _, name := range []string{"sessions.html", "session.html"} {
		parsed, err := template.ParseFS(templateFiles, "templates/layout.html", "templates/"+name)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		pages[name] = parsed
	}
	return pages, nil
}

type view struct {
	reader Reader
	pages  map[string]*template.Template
}

func (v *view) render(w http.ResponseWriter, page string, data any) {
	parsed, found := v.pages[page]
	if !found {
		http.Error(w, "no such page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := parsed.ExecuteTemplate(w, "layout", data); err != nil {
		// The status is already written by now, so this cannot become a 500. It goes nowhere useful
		// on purpose: a half rendered page is visible to the operator, who can reload.
		return
	}
}
