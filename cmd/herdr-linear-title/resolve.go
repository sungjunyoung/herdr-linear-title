package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/sungjunyoung/herdr-linear-title/internal/branch"
	"github.com/sungjunyoung/herdr-linear-title/internal/retitle"
)

// resolution is the one JSON object "resolve" prints. The Herdr desktop mod
// (herdr-app/mods/linear-title) depends on these fields.
type resolution struct {
	Status     string `json:"status"` // "ok", "no_ref" or "error"
	Identifier string `json:"identifier,omitempty"`
	Title      string `json:"title,omitempty"`
	Label      string `json:"label,omitempty"`
	Message    string `json:"message,omitempty"`
}

// resolve prints the label for branchName without touching herdr. It exits
// non-zero exactly when the printed status is "error".
func (a *app) resolve(ctx context.Context, out io.Writer, branchName string) error {
	res := resolution{Status: "error"}
	rt, err := a.load()
	if err == nil {
		res = resolveBranch(ctx, rt.retitler.Matcher, rt.cfg.Title, rt.lookup, branchName)
	} else {
		res.Message = userMessage(err)
	}
	if err := json.NewEncoder(out).Encode(res); err != nil {
		return err
	}
	if res.Status == "error" {
		return fmt.Errorf("resolve %s: %s", branchName, res.Message)
	}
	return nil
}

func resolveBranch(ctx context.Context, m *branch.Matcher, format func(identifier, title string) string, lookup retitle.LookupFunc, branchName string) resolution {
	ref, ok := m.Match(branchName)
	if !ok {
		return resolution{Status: "no_ref"}
	}
	issue, err := lookup(ctx, ref)
	if err != nil {
		return resolution{Status: "error", Message: userMessage(err)}
	}
	return resolution{
		Status:     "ok",
		Identifier: issue.Identifier,
		Title:      issue.Title,
		Label:      format(issue.Identifier, issue.Title),
	}
}
