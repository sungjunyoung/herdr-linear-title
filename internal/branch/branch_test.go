package branch

import "testing"

func TestMatch(t *testing.T) {
	m, err := NewMatcher([]string{"HOMECO", "ENG"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		branch string
		want   string // "" means no match
	}{
		{"homeco-2290", "HOMECO-2290"},
		{"HOMECO-2290", "HOMECO-2290"},
		{"sungjunyoung/homeco-2290-add-login", "HOMECO-2290"},
		{"feature/eng-7_fix", "ENG-7"},
		{"homeco-12-and-eng-34", "HOMECO-12"},
		{"xhomeco-1", ""},
		{"homeco1-1", ""},
		{"homeco-", ""},
		{"homeco-abc", ""},
		{"develop", ""},
		{"worktree/lucky-river-42", ""},
	}
	for _, tt := range tests {
		ref, ok := m.Match(tt.branch)
		got := ""
		if ok {
			got = ref.Identifier()
		}
		if got != tt.want {
			t.Errorf("Match(%q) = %q, want %q", tt.branch, got, tt.want)
		}
	}
}

func TestNewMatcherRequiresKeys(t *testing.T) {
	if _, err := NewMatcher(nil); err == nil {
		t.Fatal("NewMatcher(nil) succeeded, want error")
	}
}
