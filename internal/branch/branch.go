// Package branch extracts Linear issue references from git branch names.
package branch

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// IssueRef identifies a Linear issue by team key and number, e.g. HOMECO-2290.
type IssueRef struct {
	TeamKey string
	Number  int
}

// Identifier returns the Linear issue identifier, e.g. "HOMECO-2290".
func (r IssueRef) Identifier() string {
	return r.TeamKey + "-" + strconv.Itoa(r.Number)
}

// Matcher finds the first issue reference for a configured set of team keys.
type Matcher struct {
	re *regexp.Regexp
}

// NewMatcher returns a Matcher for teamKeys. Keys are matched case-insensitively
// anywhere in the branch, but must not be preceded by a letter or digit, so
// "sungjunyoung/homeco-2290-login" matches HOMECO while "xhomeco-1" does not.
func NewMatcher(teamKeys []string) (*Matcher, error) {
	if len(teamKeys) == 0 {
		return nil, errors.New("no team keys configured")
	}
	quoted := make([]string, len(teamKeys))
	for i, k := range teamKeys {
		quoted[i] = regexp.QuoteMeta(k)
	}
	re, err := regexp.Compile(`(?i)(?:^|[^A-Za-z0-9])(` + strings.Join(quoted, "|") + `)-([0-9]+)`)
	if err != nil {
		return nil, err
	}
	return &Matcher{re: re}, nil
}

// Match returns the first issue reference in branch.
func (m *Matcher) Match(branch string) (IssueRef, bool) {
	sub := m.re.FindStringSubmatch(branch)
	if sub == nil {
		return IssueRef{}, false
	}
	n, err := strconv.Atoi(sub[2])
	if err != nil {
		return IssueRef{}, false
	}
	return IssueRef{TeamKey: strings.ToUpper(sub[1]), Number: n}, true
}
