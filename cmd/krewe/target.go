package main

import (
	"context"
	"fmt"
	"io"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
)

// The flags a deploy target is declared with. Three values, each named, because three bare words in
// a row is an order somebody has to remember and getting it wrong writes a region into the account.
const (
	flagAccount  = "--account"
	flagRegion   = "--region"
	flagIdentity = "--identity"
	// The way back off a target. A wrong account recorded is worse than none recorded, so the door
	// that wrote it opens the other way.
	flagClear = "--clear"
)

// runTarget reads and declares where a project ships.
//
// It is not called deploy. Nothing here deploys anything, and a command that reads as though it does
// is a command somebody types expecting a release: infrastructure ships through the repository's own
// pipeline, and this is the record of which account that pipeline is aimed at.
func runTarget(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	values, rest, err := readFlags(args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: krewe target [<workspace>/<project>] [%s <id>] [%s <region>] [%s <arn>] [%s]",
			flagAccount, flagRegion, flagIdentity, flagClear)
	}
	typed := ""
	if len(rest) == 1 {
		typed = rest[0]
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if !located.HasProject() {
		return fmt.Errorf("a deploy target belongs to a project, and %s is a workspace: "+
			"say which body of work, for example krewe target %s/<project>",
			located.Path, located.Path.Workspace)
	}

	setting := values.has(flagAccount) || values.has(flagRegion) || values.has(flagIdentity)
	if setting && values.has(flagClear) {
		return fmt.Errorf("%s says a project deploys nowhere, so it cannot be given a value to deploy to: "+
			"clear it, or declare it, not both", flagClear)
	}

	project, err := client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: located.ProjectID})
	if err != nil {
		return err
	}
	target := project.GetProject().GetDeployTarget()

	switch {
	case values.has(flagClear):
		target = nil
	case setting:
		// Read the row and write it back, so declaring one value leaves the other two, the way a
		// ceiling is set one number at a time.
		asked := &quaycrewv1.DeployTarget{
			Account:  target.GetAccount(),
			Region:   target.GetRegion(),
			Identity: target.GetIdentity(),
		}
		for _, flag := range []struct {
			name string
			set  func(string)
		}{
			{flagAccount, func(v string) { asked.Account = v }},
			{flagRegion, func(v string) { asked.Region = v }},
			{flagIdentity, func(v string) { asked.Identity = v }},
		} {
			if values.has(flag.name) {
				flag.set(values.first(flag.name))
			}
		}
		target = asked
	}

	if setting || values.has(flagClear) {
		written, err := client.SetDeployTarget(ctx, &quaycrewv1.SetDeployTargetRequest{
			Project: located.ProjectID, Target: target,
		})
		if err != nil {
			return err
		}
		target = written.GetProject().GetDeployTarget()
	}

	fmt.Fprintf(out, "%s\n", located.Path)
	if target == nil {
		fmt.Fprintln(out, "deploys nowhere: this project has not said where its work ships")
		fmt.Fprintf(out, "\nsay where with krewe target %s %s 123456789012 %s eu-west-2 %s arn:aws:iam::123456789012:role/<name>\n",
			located.Path, flagAccount, flagRegion, flagIdentity)
		return nil
	}
	fmt.Fprintf(out, "account   %s\n", target.GetAccount())
	fmt.Fprintf(out, "region    %s\n", target.GetRegion())
	fmt.Fprintf(out, "identity  %s\n", target.GetIdentity())
	return nil
}

// targetFlagsTaken is the flags krewe target takes, for the refusal that guards every other command.
func targetFlagsTaken() map[string]bool {
	return map[string]bool{
		flagAccount: true, flagRegion: true, flagIdentity: true, flagClear: true,
	}
}

// deploysTo is where a project ships, for the end of a listing row, and nothing at all for a project
// that has not said.
//
// The account and the region only. The identity repeats the account and is three times as wide as
// anything else in the row, and the whole of it is one command away.
func deploysTo(target *quaycrewv1.DeployTarget) string {
	if target == nil {
		return ""
	}
	return "  " + target.GetAccount() + "/" + target.GetRegion()
}
