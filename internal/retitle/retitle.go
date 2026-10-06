// Package retitle renames worktree workspaces after the Linear issue their
// branch refers to.
package retitle

import (
	"context"
	"path/filepath"

	"github.com/sungjunyoung/herdr-linear-title/internal/branch"
	"github.com/sungjunyoung/herdr-linear-title/internal/herdr"
	"github.com/sungjunyoung/herdr-linear-title/internal/labels"
	"github.com/sungjunyoung/herdr-linear-title/internal/linear"
)

// Mode controls whether labels chosen by the user are preserved.
type Mode int

const (
	// Auto renames only workspaces that still carry herdr's default label or
	// the label this plugin wrote last. Used by event hooks.
	Auto Mode = iota
	// Force always renames. Used by explicit user actions.
	Force
)

// Outcome describes what Retitle did.
type Outcome int

// Retitle outcomes.
const (
	// NoIssueRef: the branch has no configured team key reference.
	NoIssueRef Outcome = iota
	// KeptUserLabel: Auto mode found a label the user chose.
	KeptUserLabel
	// Unchanged: the workspace already has the issue label.
	Unchanged
	// Renamed: the workspace label was changed.
	Renamed
)

// Target is a worktree workspace to retitle.
type Target struct {
	WorkspaceID  string
	Branch       string
	CheckoutPath string
}

// Workspaces is the subset of herdr operations Retitle needs.
type Workspaces interface {
	WorkspaceGet(ctx context.Context, id string) (herdr.Workspace, error)
	WorkspaceRename(ctx context.Context, id, label string) error
}

// LookupFunc fetches the issue for a reference. It must return an error
// wrapping linear.ErrNotFound when the issue does not exist.
type LookupFunc func(ctx context.Context, ref branch.IssueRef) (linear.Issue, error)

// Retitler applies issue titles to workspace labels.
type Retitler struct {
	Workspaces Workspaces
	Matcher    *branch.Matcher
	Labels     labels.Store
	// Format renders the label for an issue.
	Format func(identifier, title string) string
}

// Result reports the outcome for one target.
type Result struct {
	Outcome Outcome
	// Ref is set unless Outcome is NoIssueRef.
	Ref branch.IssueRef
	// Label is the workspace label after the call.
	Label string
}

// Retitle renames t's workspace to the formatted title of the issue its
// branch refers to. In Auto mode the user-label check runs before lookup, so
// workspaces the user renamed cost no Linear request.
func (r *Retitler) Retitle(ctx context.Context, t Target, mode Mode, lookup LookupFunc) (Result, error) {
	ref, ok := r.Matcher.Match(t.Branch)
	if !ok {
		return Result{Outcome: NoIssueRef}, nil
	}
	ws, err := r.Workspaces.WorkspaceGet(ctx, t.WorkspaceID)
	if err != nil {
		return Result{}, err
	}
	res := Result{Ref: ref, Label: ws.Label}

	if mode == Auto {
		auto, err := r.isAutomaticLabel(ws.Label, t)
		if err != nil {
			return Result{}, err
		}
		if !auto {
			res.Outcome = KeptUserLabel
			return res, nil
		}
	}

	issue, err := lookup(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	label := r.Format(issue.Identifier, issue.Title)
	res.Outcome = Unchanged
	if label != ws.Label {
		if err := r.Workspaces.WorkspaceRename(ctx, t.WorkspaceID, label); err != nil {
			return Result{}, err
		}
		res.Outcome = Renamed
		res.Label = label
	}
	// Record even when unchanged so a label that already matched (e.g. set
	// before the plugin was installed) is treated as plugin-owned later.
	if err := r.Labels.Set(t.CheckoutPath, label); err != nil {
		return Result{}, err
	}
	return res, nil
}

// isAutomaticLabel reports whether label was not chosen by the user: herdr
// labels new worktree workspaces with the branch, or with the checkout
// directory name when the branch contains "/".
func (r *Retitler) isAutomaticLabel(label string, t Target) (bool, error) {
	if label == t.Branch || label == filepath.Base(t.CheckoutPath) {
		return true, nil
	}
	recorded, ok, err := r.Labels.Get(t.CheckoutPath)
	if err != nil {
		return false, err
	}
	return ok && label == recorded, nil
}
