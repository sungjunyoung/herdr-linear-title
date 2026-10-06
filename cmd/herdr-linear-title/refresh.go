package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sungjunyoung/herdr-linear-title/internal/branch"
	"github.com/sungjunyoung/herdr-linear-title/internal/herdr"
	"github.com/sungjunyoung/herdr-linear-title/internal/labels"
	"github.com/sungjunyoung/herdr-linear-title/internal/linear"
	"github.com/sungjunyoung/herdr-linear-title/internal/retitle"
)

// refresh retitles one workspace, overwriting any label.
func (a *app) refresh(ctx context.Context, workspaceID string) error {
	if workspaceID == "" {
		return errors.New("no current workspace")
	}
	rt, err := a.load()
	if err != nil {
		return err
	}
	ws, err := a.herdr.WorkspaceGet(ctx, workspaceID)
	if err != nil {
		return err
	}
	if ws.Worktree == nil {
		return fmt.Errorf("workspace %q is not a git worktree", ws.Label)
	}
	worktrees, err := a.herdr.WorktreeList(ctx, ws.ID)
	if err != nil {
		return err
	}
	target, ok := targetFor(ws, worktrees)
	if !ok {
		return fmt.Errorf("workspace %q has no checked-out branch", ws.Label)
	}

	res, err := rt.retitler.Retitle(ctx, target, retitle.Force, rt.lookup)
	if err != nil {
		return err
	}
	if res.Outcome == retitle.NoIssueRef {
		return fmt.Errorf("branch %q has no %s issue reference", target.Branch, strings.Join(rt.cfg.TeamKeys, "/"))
	}
	a.notify(ctx, describe(res))
	return nil
}

// refreshAll retitles every open linked-worktree workspace, overwriting any
// label. Issues are fetched with one request per team key.
func (a *app) refreshAll(ctx context.Context) error {
	rt, err := a.load()
	if err != nil {
		return err
	}
	targets, err := a.linkedWorktreeTargets(ctx)
	if err != nil {
		return err
	}

	numbers := map[string][]int{}
	for _, t := range targets {
		if ref, ok := rt.retitler.Matcher.Match(t.Branch); ok {
			numbers[ref.TeamKey] = append(numbers[ref.TeamKey], ref.Number)
		}
	}
	issues := map[string]linear.Issue{}
	for key, nums := range numbers {
		found, err := rt.linear.IssuesByNumber(ctx, key, nums)
		if err != nil {
			return err
		}
		for _, is := range found {
			issues[is.Identifier] = is
		}
	}
	lookup := func(_ context.Context, ref branch.IssueRef) (linear.Issue, error) {
		is, ok := issues[ref.Identifier()]
		if !ok {
			return linear.Issue{}, fmt.Errorf("%s: %w", ref.Identifier(), linear.ErrNotFound)
		}
		return is, nil
	}

	var renamed, unchanged, skipped int
	var failures []string
	for _, t := range targets {
		res, err := rt.retitler.Retitle(ctx, t, retitle.Force, lookup)
		if err != nil {
			failures = append(failures, err.Error())
			fmt.Fprintf(a.log, "%s: %v\n", t.Branch, err)
			continue
		}
		fmt.Fprintf(a.log, "%s: %s\n", t.Branch, describe(res))
		switch res.Outcome {
		case retitle.Renamed:
			renamed++
		case retitle.Unchanged:
			unchanged++
		default:
			skipped++
		}
	}

	summary := fmt.Sprintf("%d renamed, %d unchanged, %d without issue", renamed, unchanged, skipped)
	if len(failures) > 0 {
		// Returned rather than notified here: run's reportErr notifies once
		// and exits non-zero so herdr logs the failure.
		return fmt.Errorf("%s, failed: %s", summary, strings.Join(failures, "; "))
	}
	a.notify(ctx, summary)
	return nil
}

// linkedWorktreeTargets lists open linked-worktree workspaces with their
// branches, calling worktree list once per repository.
func (a *app) linkedWorktreeTargets(ctx context.Context) ([]retitle.Target, error) {
	all, err := a.herdr.WorkspaceList(ctx)
	if err != nil {
		return nil, err
	}
	byRepo := map[string][]herdr.Workspace{}
	var repos []string
	for _, ws := range all {
		if ws.Worktree == nil || !ws.Worktree.IsLinkedWorktree {
			continue
		}
		key := ws.Worktree.RepoKey
		if _, seen := byRepo[key]; !seen {
			repos = append(repos, key)
		}
		byRepo[key] = append(byRepo[key], ws)
	}

	var targets []retitle.Target
	for _, repo := range repos {
		group := byRepo[repo]
		worktrees, err := a.herdr.WorktreeList(ctx, group[0].ID)
		if err != nil {
			return nil, err
		}
		for _, ws := range group {
			if t, ok := targetFor(ws, worktrees); ok {
				targets = append(targets, t)
			}
		}
	}
	return targets, nil
}

// targetFor finds the branch checked out in ws among its repository's
// worktrees.
func targetFor(ws herdr.Workspace, worktrees []herdr.Worktree) (retitle.Target, bool) {
	checkout := labels.Canonical(ws.Worktree.CheckoutPath)
	for _, wt := range worktrees {
		if labels.Canonical(wt.Path) != checkout {
			continue
		}
		if wt.IsDetached || wt.Branch == "" {
			return retitle.Target{}, false
		}
		return retitle.Target{WorkspaceID: ws.ID, Branch: wt.Branch, CheckoutPath: wt.Path}, true
	}
	return retitle.Target{}, false
}
