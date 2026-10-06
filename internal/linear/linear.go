// Package linear is a minimal Linear GraphQL client for reading issue titles.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Endpoint is the Linear GraphQL API URL.
const Endpoint = "https://api.linear.app/graphql"

// maxBatch bounds the issue numbers per IssuesByNumber request, matching the
// page size requested so no result is silently cut off.
const maxBatch = 250

var (
	// ErrNotFound means Linear has no issue with the requested identifier.
	ErrNotFound = errors.New("issue not found")
	// ErrUnauthorized means Linear rejected the credentials.
	ErrUnauthorized = errors.New("linear rejected the credentials")
)

// Authorizer supplies the Authorization header value for each request.
type Authorizer interface {
	Authorization(ctx context.Context) (string, error)
}

// Issue is the subset of a Linear issue the plugin needs.
type Issue struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// Client calls the Linear GraphQL API.
type Client struct {
	HTTP     *http.Client
	Endpoint string
	Auth     Authorizer
}

// New returns a Client for the public Linear API.
func New(auth Authorizer) *Client {
	return &Client{
		HTTP:     &http.Client{Timeout: 15 * time.Second},
		Endpoint: Endpoint,
		Auth:     auth,
	}
}

// Viewer returns the display name of the authenticated user.
func (c *Client) Viewer(ctx context.Context) (string, error) {
	var out struct {
		Viewer struct {
			Name string `json:"name"`
		} `json:"viewer"`
	}
	if err := c.do(ctx, `query { viewer { name } }`, nil, &out); err != nil {
		return "", err
	}
	return out.Viewer.Name, nil
}

// Issue fetches one issue by identifier such as "HOMECO-2290". It returns an
// error wrapping ErrNotFound when the issue does not exist.
func (c *Client) Issue(ctx context.Context, identifier string) (Issue, error) {
	var out struct {
		Issue Issue `json:"issue"`
	}
	err := c.do(ctx, `query($id: String!) { issue(id: $id) { identifier title } }`,
		map[string]any{"id": identifier}, &out)
	if err != nil {
		return Issue{}, fmt.Errorf("%s: %w", identifier, err)
	}
	return out.Issue, nil
}

// IssuesByNumber fetches the issues of one team by number. Numbers without an
// issue are simply absent from the result.
//
// A single query with one aliased issue(id:) field per issue is not used
// because Linear nulls the whole response when any alias is not found.
func (c *Client) IssuesByNumber(ctx context.Context, teamKey string, numbers []int) ([]Issue, error) {
	const query = `query($key: String!, $numbers: [Float!], $first: Int!) {
  issues(first: $first, filter: { team: { key: { eq: $key } }, number: { in: $numbers } }) {
    nodes { identifier title }
  }
}`
	var issues []Issue
	for start := 0; start < len(numbers); start += maxBatch {
		chunk := numbers[start:min(start+maxBatch, len(numbers))]
		var out struct {
			Issues struct {
				Nodes []Issue `json:"nodes"`
			} `json:"issues"`
		}
		vars := map[string]any{"key": teamKey, "numbers": chunk, "first": maxBatch}
		if err := c.do(ctx, query, vars, &out); err != nil {
			return nil, err
		}
		issues = append(issues, out.Issues.Nodes...)
	}
	return issues, nil
}

type gqlError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code string `json:"code"`
	} `json:"extensions"`
}

func (c *Client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	authz, err := c.Auth.Authorization(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables,omitempty"`
	}{query, vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authz)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("linear request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("linear response: %w", err)
	}

	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		if resp.StatusCode == http.StatusUnauthorized {
			return ErrUnauthorized
		}
		return fmt.Errorf("linear: HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	// Linear reports most failures, including "not found", as HTTP 200 with
	// an errors array, so errors must be inspected before the status code.
	if len(env.Errors) > 0 {
		return classify(resp.StatusCode, env.Errors)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("linear: HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	return json.Unmarshal(env.Data, out)
}

func classify(status int, errs []gqlError) error {
	for _, e := range errs {
		switch {
		case e.Extensions.Code == "AUTHENTICATION_ERROR":
			return fmt.Errorf("%w: %s", ErrUnauthorized, e.Message)
		case e.Extensions.Code == "INPUT_ERROR" && strings.HasPrefix(e.Message, "Entity not found"):
			return ErrNotFound
		}
	}
	if status == http.StatusUnauthorized {
		return fmt.Errorf("%w: %s", ErrUnauthorized, errs[0].Message)
	}
	return fmt.Errorf("linear: %s", errs[0].Message)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
