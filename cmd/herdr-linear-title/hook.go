package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sungjunyoung/herdr-linear-title/internal/branch"
	"github.com/sungjunyoung/herdr-linear-title/internal/herdr"
	"github.com/sungjunyoung/herdr-linear-title/internal/linear"
	"github.com/sungjunyoung/herdr-linear-title/internal/retitle"
)

// worktreeEvent is HERDR_PLUGIN_EVENT_JSON for worktree.created and
// worktree.opened.
type worktreeEvent struct {
	Data struct {
		Type        string          `json:"type"`
		AlreadyOpen bool            `json:"already_open"`
		Workspace   herdr.Workspace `json:"workspace"`
		Worktree    herdr.Worktree  `json:"worktree"`
	} `json:"data"`
}

// hook retitles the workspace of a created or (re)opened worktree unless the
// user already chose its label.
func (a *app) hook(ctx context.Context, eventJSON string) error {
	if eventJSON == "" {
		return errors.New("HERDR_PLUGIN_EVENT_JSON is empty")
	}
	var ev worktreeEvent
	if err := json.Unmarshal([]byte(eventJSON), &ev); err != nil {
		return fmt.Errorf("decode event: %w", err)
	}
	d := ev.Data
	switch {
	case d.Type != "worktree_created" && d.Type != "worktree_opened":
		return fmt.Errorf("unsupported event type %q", d.Type)
	case d.AlreadyOpen:
		// Focusing an open workspace keeps its label; nothing to do.
		return nil
	case d.Worktree.IsDetached || d.Worktree.Branch == "":
		return nil
	}

	rt, err := a.load()
	if err != nil {
		return err
	}
	target := retitle.Target{
		WorkspaceID:  d.Workspace.ID,
		Branch:       d.Worktree.Branch,
		CheckoutPath: d.Worktree.Path,
	}
	res, err := rt.retitler.Retitle(ctx, target, retitle.Auto, rt.lookup)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.log, "%s: %s\n", target.Branch, describe(res))
	return nil
}

// lookup fetches a single issue from Linear.
func (rt *runtime) lookup(ctx context.Context, ref branch.IssueRef) (linear.Issue, error) {
	return rt.linear.Issue(ctx, ref.Identifier())
}

func describe(res retitle.Result) string {
	switch res.Outcome {
	case retitle.NoIssueRef:
		return "no issue reference"
	case retitle.KeptUserLabel:
		return fmt.Sprintf("kept user label %q", res.Label)
	case retitle.Unchanged:
		return fmt.Sprintf("already %q", res.Label)
	default:
		return fmt.Sprintf("renamed to %q", res.Label)
	}
}
