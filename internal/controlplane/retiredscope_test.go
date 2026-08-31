package controlplane_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/name"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// scopeField is the field a caller says the level in. A call that carries one is a call an operator
// can type the level word at, and every one of them has to refuse the word the level used to take.
const scopeField = "scope"

// scopeTaking is every method of the control plane whose request carries a scope, read off the
// protocol rather than off a list somebody keeps.
//
// The list was written out by hand and had eight entries, which was every scope taking call on the
// day it was written. A ninth call added later would carry the field, forget the refusal, and this
// suite would stay green: the guard covered the calls somebody remembered rather than the class.
func scopeTaking(t *testing.T) []protoreflect.MethodDescriptor {
	t.Helper()
	service := quaycrewv1.File_quaycrew_v1_controlplane_proto.Services().ByName("ControlPlaneService")
	if service == nil {
		t.Fatal("the control plane service is not in its own descriptor, so nothing below reads the protocol")
	}
	var found []protoreflect.MethodDescriptor
	for i := 0; i < service.Methods().Len(); i++ {
		method := service.Methods().Get(i)
		field := method.Input().Fields().ByName(scopeField)
		if field != nil && field.Kind() == protoreflect.StringKind {
			found = append(found, method)
		}
	}
	// A walk that finds nothing runs no case and reports success, which is the shape this whole test
	// exists to refuse.
	if len(found) == 0 {
		t.Fatal("no call carries a scope, so this proves nothing about the word")
	}
	t.Logf("%d calls carry a scope", len(found))
	return found
}

// Every call that takes a scope refuses the word the level above every workspace used to take.
//
// The tool is not the only way in, so this goes over a real connection rather than calling the
// methods in this process. A tool built before the word moved reaches the control plane, and so does
// every channel: without the refusal here the call is read as a workspace and comes back saying no
// such workspace, which says nothing about the word having changed.
func TestEveryScopeRefusesTheWordTheLevelUsedToTake(t *testing.T) {
	conn := dialControlPlane(t)
	// Both spellings, because a name is lowercase and neither of these can be anything but the word.
	for _, typed := range []string{name.Retired, strings.ToUpper(name.Retired)} {
		for _, method := range scopeTaking(t) {
			t.Run(fmt.Sprintf("%s/%s", method.Name(), typed), func(t *testing.T) {
				err := callWithScope(t, conn, method, typed)
				if err == nil {
					t.Fatalf("%s took a scope of %q, so the word stopped working quietly", method.Name(), typed)
				}
				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf("%s answered %v, want InvalidArgument", method.Name(), status.Code(err))
				}
				// The refusal itself, word for word, and not merely a message that happens to carry
				// "system" in it. SetContext already refused an unknown scope by listing the three it
				// takes, so an assertion looking only for the new word passed there without the
				// refusal below it existing at all.
				if want := name.RefuseRetired(name.Retired).Error(); !strings.Contains(err.Error(), want) {
					t.Fatalf("%s refused with %q, want it to carry %q", method.Name(), err, want)
				}
			})
		}
	}
}

// And the word it became still works, on the same calls, or the refusal above would be proving that
// both words are broken rather than that one moved.
func TestEveryScopeStillTakesTheWordTheLevelHasNow(t *testing.T) {
	ctx := context.Background()
	s := newServer(&model.FakeRunner{})
	if _, err := s.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
		Scope: name.System, Key: "CLAUDE_CODE_OAUTH_TOKEN", Value: "tok-xyz",
	}); err != nil {
		t.Fatalf("SetSecret on %q: %v", name.System, err)
	}
	if _, err := s.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: name.System, Body: "everything this system does",
	}); err != nil {
		t.Fatalf("SetContext on %q: %v", name.System, err)
	}
}

// callWithScope sends one call with nothing set but the scope, so what comes back is the answer to
// the word and not to a missing name. Every scope taking call reads the scope first for that reason.
func callWithScope(t *testing.T, conn *grpc.ClientConn, method protoreflect.MethodDescriptor, typed string) error {
	t.Helper()
	request := dynamicpb.NewMessage(method.Input())
	request.Set(method.Input().Fields().ByName(scopeField), protoreflect.ValueOfString(typed))
	response := dynamicpb.NewMessage(method.Output())
	full := fmt.Sprintf("/%s/%s", method.Parent().FullName(), method.Name())
	return conn.Invoke(context.Background(), full, request, response)
}

// dialControlPlane serves a control plane over an in memory listener and dials it, so a call travels
// the wire a tool travels rather than being a method call in this process.
func dialControlPlane(t *testing.T) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	quaycrewv1.RegisterControlPlaneServiceServer(server, newServer(&model.FakeRunner{}))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
