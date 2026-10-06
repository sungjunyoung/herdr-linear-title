package retitle

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sungjunyoung/herdr-linear-title/internal/branch"
	"github.com/sungjunyoung/herdr-linear-title/internal/herdr"
	"github.com/sungjunyoung/herdr-linear-title/internal/labels"
	"github.com/sungjunyoung/herdr-linear-title/internal/linear"
)

type fakeWorkspaces struct {
	label   string
	renames int
}

func (f *fakeWorkspaces) WorkspaceGet(_ context.Context, id string) (herdr.Workspace, error) {
	return herdr.Workspace{ID: id, Label: f.label}, nil
}

func (f *fakeWorkspaces) WorkspaceRename(_ context.Context, _, label string) error {
	f.label = label
	f.renames++
	return nil
}

type harness struct {
	ws      *fakeWorkspaces
	r       *Retitler
	lookups int
	target  Target
}

func newHarness(t *testing.T, label, branchName string) *harness {
	t.Helper()
	m, err := branch.NewMatcher([]string{"HOMECO"})
	if err != nil {
		t.Fatal(err)
	}
	ws := &fakeWorkspaces{label: label}
	return &harness{
		ws: ws,
		r: &Retitler{
			Workspaces: ws,
			Matcher:    m,
			Labels:     labels.Store{Dir: t.TempDir()},
			Format:     func(id, title string) string { return id + " " + title },
		},
		target: Target{
			WorkspaceID:  "w1",
			Branch:       branchName,
			CheckoutPath: filepath.Join(t.TempDir(), "sungjunyoung-homeco-1-login"),
		},
	}
}

func (h *harness) run(t *testing.T, mode Mode, title string) Result {
	t.Helper()
	res, err := h.r.Retitle(context.Background(), h.target, mode, func(_ context.Context, ref branch.IssueRef) (linear.Issue, error) {
		h.lookups++
		return linear.Issue{Identifier: ref.Identifier(), Title: title}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAutoRenamesDefaultLabels(t *testing.T) {
	for name, label := range map[string]string{
		"branch label":       "sungjunyoung/homeco-1-login",
		"checkout dir label": "sungjunyoung-homeco-1-login",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, label, "sungjunyoung/homeco-1-login")
			res := h.run(t, Auto, "Login")
			if res.Outcome != Renamed || h.ws.label != "HOMECO-1 Login" {
				t.Fatalf("result = %+v, label = %q", res, h.ws.label)
			}
		})
	}
}

func TestAutoKeepsUserLabelWithoutLookup(t *testing.T) {
	h := newHarness(t, "my own name", "homeco-1")
	res := h.run(t, Auto, "Login")
	if res.Outcome != KeptUserLabel || h.ws.renames != 0 || h.lookups != 0 {
		t.Fatalf("result = %+v, renames = %d, lookups = %d", res, h.ws.renames, h.lookups)
	}
}

// After the plugin renamed a workspace, a later automatic run must still own
// the label, e.g. to pick up an issue title change on reopen.
func TestAutoUpdatesLabelWrittenByPlugin(t *testing.T) {
	h := newHarness(t, "homeco-1", "homeco-1")
	h.run(t, Auto, "Old title")
	res := h.run(t, Auto, "New title")
	if res.Outcome != Renamed || h.ws.label != "HOMECO-1 New title" {
		t.Fatalf("result = %+v, label = %q", res, h.ws.label)
	}

	h.ws.label = "user renamed it"
	if res := h.run(t, Auto, "Newer title"); res.Outcome != KeptUserLabel {
		t.Fatalf("result = %+v, want user label kept", res)
	}
}

func TestForceOverwritesUserLabel(t *testing.T) {
	h := newHarness(t, "my own name", "homeco-1")
	res := h.run(t, Force, "Login")
	if res.Outcome != Renamed || h.ws.label != "HOMECO-1 Login" {
		t.Fatalf("result = %+v, label = %q", res, h.ws.label)
	}
}

func TestUnchangedSkipsRename(t *testing.T) {
	h := newHarness(t, "HOMECO-1 Login", "homeco-1")
	res := h.run(t, Force, "Login")
	if res.Outcome != Unchanged || h.ws.renames != 0 {
		t.Fatalf("result = %+v, renames = %d", res, h.ws.renames)
	}
	// The matching label now counts as plugin-owned for automatic runs.
	if res := h.run(t, Auto, "Login v2"); res.Outcome != Renamed {
		t.Fatalf("auto after unchanged = %+v, want Renamed", res)
	}
}

func TestNoIssueRef(t *testing.T) {
	h := newHarness(t, "develop", "develop")
	res := h.run(t, Force, "x")
	if res.Outcome != NoIssueRef || h.lookups != 0 {
		t.Fatalf("result = %+v, lookups = %d", res, h.lookups)
	}
}

func TestLookupErrorKeepsLabel(t *testing.T) {
	h := newHarness(t, "homeco-1", "homeco-1")
	_, err := h.r.Retitle(context.Background(), h.target, Auto, func(context.Context, branch.IssueRef) (linear.Issue, error) {
		return linear.Issue{}, linear.ErrNotFound
	})
	if !errors.Is(err, linear.ErrNotFound) || h.ws.renames != 0 {
		t.Fatalf("err = %v, renames = %d", err, h.ws.renames)
	}
}
