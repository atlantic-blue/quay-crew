package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/skill"
)

// runSkill drives the crew's skills from the command line: what it can do, what a workspace holds, and
// giving or taking one away.
//
// Importing reads the directory here rather than sending a path, because the control plane may be in a
// container that cannot see the operator's machine. The client is a pipe: it reads the files and sends
// them, and every rule about what a skill is lives on the other side.
func runSkill(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quay skill <import|list|attach|detach>")
	}
	switch args[0] {
	case "import":
		return runSkillImport(ctx, client, args[1:], out)
	case "list":
		return runSkillList(ctx, client, args[1:], out)
	case "attach":
		return runSkillAttach(ctx, client, args[1:], out)
	case "detach":
		return runSkillDetach(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("usage: quay skill <import|list|attach|detach>")
	}
}

func runSkillImport(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay skill import <directory>")
	}
	// Read and validate here as well as on the other side, so a malformed skill is refused before it is
	// sent anywhere. The control plane refuses it too, and that is the check that counts.
	loaded, err := skill.One(args[0])
	if err != nil {
		return err
	}

	files := make([]*quaycrewv1.SkillFile, 0, len(loaded.Files))
	for _, file := range loaded.Files {
		files = append(files, &quaycrewv1.SkillFile{
			Path:       file.Path,
			Body:       file.Body,
			Executable: file.Executable,
		})
	}
	resp, err := client.ImportSkill(ctx, &quaycrewv1.ImportSkillRequest{Files: files})
	if err != nil {
		return err
	}
	imported := resp.GetSkill()
	fmt.Fprintf(out, "imported %s version %d: %s\n",
		imported.GetName(), imported.GetVersion(), imported.GetSummary())
	fmt.Fprintf(out, "attach it with: quay skill attach %s\n", imported.GetName())
	return nil
}

func runSkillList(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: quay skill list [<workspace, or workspace/project/thread>]")
	}

	// With no address, this is what the crew holds. With a workspace, what that workspace holds.
	// With a thread, what that thread actually holds: the same answer its sandbox is built from,
	// crew skills included, which no attachment row records.
	request := &quaycrewv1.ListSkillsRequest{}
	where := "the crew"
	if typed != "" {
		located, err := locate(ctx, client, typed)
		if err != nil {
			return err
		}
		request.Workspace = located.WorkspaceID
		where = located.Path.Workspace
		if located.ThreadID != "" {
			// The address resolves to the thread's handle; holdings hang off the thread itself,
			// so find its row by the handle.
			threads, err := client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{Project: located.ProjectID})
			if err != nil {
				return err
			}
			for _, thread := range threads.GetThreads() {
				if thread.GetHandle() == located.ThreadID {
					request = &quaycrewv1.ListSkillsRequest{Thread: thread.GetId()}
					where = "thread " + located.ThreadID
					break
				}
			}
			if request.GetThread() == "" {
				return fmt.Errorf("thread %q is not in %s/%s", located.ThreadID, located.Path.Workspace, located.Path.Project)
			}
		}
	}

	resp, err := client.ListSkills(ctx, request)
	if err != nil {
		return err
	}
	if len(resp.GetSkills()) == 0 {
		fmt.Fprintf(out, "%s holds no skills\n", where)
		return nil
	}
	for _, held := range resp.GetSkills() {
		fmt.Fprintf(out, "%-16s v%-3d %s\n", held.GetName(), held.GetVersion(), held.GetSummary())
		// Held and not given, which is the line that stops somebody hunting for a skill the model
		// never had. It goes first because it is the reason the rest of the entry does not apply.
		if held.GetCrew() {
			fmt.Fprintf(out, "%-16s      held by the crew, so every workspace has it\n", "")
		}
		if held.GetLeftOut() != "" {
			fmt.Fprintf(out, "%-16s      left out: %s\n", "", held.GetLeftOut())
		}
		if len(held.GetBinaries()) > 0 {
			fmt.Fprintf(out, "%-16s      needs: %s\n", "", strings.Join(held.GetBinaries(), " "))
		}
		for _, secret := range held.GetSecrets() {
			fmt.Fprintf(out, "%-16s      %s: %s\n", "", secret.GetName(), secret.GetPurpose())
		}
	}
	return nil
}

func runSkillAttach(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	name, typed, err := skillAndAddress(args, "attach")
	if err != nil {
		return err
	}
	// "crew" where a workspace goes, the same word quay context set takes, and it means the same
	// thing: everything this crew does, including the workspaces made after today.
	if typed == crewScope {
		resp, err := client.AttachSkill(ctx, &quaycrewv1.AttachSkillRequest{
			Scope: crewScope, Name: name,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "the crew holds %s version %d, so every workspace has it\n",
			resp.GetSkill().GetName(), resp.GetSkill().GetVersion())
		for _, secret := range resp.GetSkill().GetSecrets() {
			fmt.Fprintf(out, "it needs %s in each workspace that is to use it: %s\n",
				secret.GetName(), secret.GetPurpose())
		}
		fmt.Fprintln(out, "a workspace without those secrets set is left out of this one skill, and its turns still run")
		return nil
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.AttachSkill(ctx, &quaycrewv1.AttachSkillRequest{
		Workspace: located.WorkspaceID, Name: name,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s holds %s version %d\n",
		located.Path.Workspace, resp.GetSkill().GetName(), resp.GetSkill().GetVersion())
	for _, secret := range resp.GetSkill().GetSecrets() {
		fmt.Fprintf(out, "it needs %s: %s\n", secret.GetName(), secret.GetPurpose())
	}
	fmt.Fprintln(out, "threads already running keep what their sandbox was born with; they show as stale in the listing until restarted")
	return nil
}

func runSkillDetach(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	name, typed, err := skillAndAddress(args, "detach")
	if err != nil {
		return err
	}
	if typed == crewScope {
		if _, err := client.DetachSkill(ctx, &quaycrewv1.DetachSkillRequest{
			Scope: crewScope, Name: name,
		}); err != nil {
			return err
		}
		fmt.Fprintf(out, "the crew no longer holds %s\n", name)
		fmt.Fprintln(out, "a workspace that attached it for itself keeps it")
		return nil
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if _, err := client.DetachSkill(ctx, &quaycrewv1.DetachSkillRequest{
		Workspace: located.WorkspaceID, Name: name,
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s no longer holds %s\n", located.Path.Workspace, name)
	fmt.Fprintln(out, "threads already running keep what their sandbox was born with; they show as stale in the listing until restarted")
	return nil
}

// skillAndAddress reads the two shapes these commands take: a skill name on its own, acting where the
// operator already is, or a workspace and a skill name.
// crewScope is the word an address takes to mean the whole crew rather than one workspace, the same
// one quay context set already takes.
const crewScope = "crew"

func skillAndAddress(args []string, verb string) (name, typed string, err error) {
	switch len(args) {
	case 1:
		return args[0], "", nil
	case 2:
		return args[1], args[0], nil
	default:
		return "", "", fmt.Errorf("usage: quay skill %s [<workspace>] <name>", verb)
	}
}
