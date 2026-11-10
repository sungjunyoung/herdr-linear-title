package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/sungjunyoung/herdr-linear-title/internal/branch"
	"github.com/sungjunyoung/herdr-linear-title/internal/config"
	"github.com/sungjunyoung/herdr-linear-title/internal/linear"
)

func TestResolveBranch(t *testing.T) {
	m, err := branch.NewMatcher([]string{"HOMECO"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{TitleFormat: config.DefaultTitleFormat}
	lookup := func(_ context.Context, ref branch.IssueRef) (linear.Issue, error) {
		if ref.Number == 401 {
			return linear.Issue{}, linear.ErrUnauthorized
		}
		if ref.Number == 404 {
			return linear.Issue{}, fmt.Errorf("%s: %w", ref.Identifier(), linear.ErrNotFound)
		}
		return linear.Issue{Identifier: ref.Identifier(), Title: "Fix login"}, nil
	}

	tests := []struct {
		branch string
		want   resolution
	}{
		{"sungjunyoung/homeco-2290-login", resolution{Status: "ok", Identifier: "HOMECO-2290", Title: "Fix login", Label: "HOMECO-2290 Fix login"}},
		{"main", resolution{Status: "no_ref"}},
		{"homeco-401", resolution{Status: "error", Message: userMessage(linear.ErrUnauthorized), NeedsSetup: true}},
		{"homeco-404", resolution{Status: "error", Message: "HOMECO-404: issue not found"}},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := resolveBranch(context.Background(), m, cfg.Title, lookup, tt.branch)
			if got != tt.want {
				t.Errorf("resolveBranch(%q) = %+v, want %+v", tt.branch, got, tt.want)
			}
		})
	}
}
