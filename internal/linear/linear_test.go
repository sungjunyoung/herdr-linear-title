package linear

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type staticAuth string

func (s staticAuth) Authorization(context.Context) (string, error) { return string(s), nil }

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(staticAuth("Bearer tok"))
	c.Endpoint = srv.URL
	return c
}

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestIssue(t *testing.T) {
	var gotAuth string
	var gotVars map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotVars = req.Variables
		respond(200, `{"data":{"issue":{"identifier":"HOMECO-1","title":"Title"}}}`)(w, r)
	})

	is, err := c.Issue(context.Background(), "HOMECO-1")
	if err != nil {
		t.Fatal(err)
	}
	if is != (Issue{Identifier: "HOMECO-1", Title: "Title"}) {
		t.Errorf("Issue() = %+v", is)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization header = %q", gotAuth)
	}
	if gotVars["id"] != "HOMECO-1" {
		t.Errorf("variables = %v", gotVars)
	}
}

// Response shapes below were captured from the real Linear API.
func TestIssueErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{
			name:   "not found is HTTP 200 with INPUT_ERROR",
			status: 200,
			body:   `{"errors":[{"message":"Entity not found: Issue","extensions":{"type":"invalid input","code":"INPUT_ERROR","statusCode":400}}],"data":null}`,
			want:   ErrNotFound,
		},
		{
			name:   "authentication error",
			status: 400,
			body:   `{"errors":[{"message":"Authentication required, not authenticated","extensions":{"type":"authentication error","code":"AUTHENTICATION_ERROR","statusCode":401}}]}`,
			want:   ErrUnauthorized,
		},
		{
			name:   "plain 401",
			status: 401,
			body:   `not json`,
			want:   ErrUnauthorized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, respond(tt.status, tt.body))
			_, err := c.Issue(context.Background(), "HOMECO-1")
			if !errors.Is(err, tt.want) {
				t.Fatalf("Issue() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestIssueOtherGraphQLErrorIsNotNotFound(t *testing.T) {
	c := newTestClient(t, respond(200, `{"errors":[{"message":"Rate limit exceeded","extensions":{"code":"RATELIMITED"}}]}`))
	_, err := c.Issue(context.Background(), "HOMECO-1")
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Issue() error = %v, want a generic error", err)
	}
}

func TestIssuesByNumberChunks(t *testing.T) {
	var batches [][]float64
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				Key     string    `json:"key"`
				Numbers []float64 `json:"numbers"`
				First   int       `json:"first"`
			} `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Variables.Key != "HOMECO" || req.Variables.First < len(req.Variables.Numbers) {
			t.Errorf("unexpected variables %+v", req.Variables)
		}
		batches = append(batches, req.Variables.Numbers)
		respond(200, `{"data":{"issues":{"nodes":[{"identifier":"HOMECO-1","title":"t"}]}}}`)(w, r)
	})

	numbers := make([]int, maxBatch+1)
	for i := range numbers {
		numbers[i] = i + 1
	}
	issues, err := c.IssuesByNumber(context.Background(), "HOMECO", numbers)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || len(batches[0]) != maxBatch || len(batches[1]) != 1 {
		t.Fatalf("batch sizes = %d/%v", len(batches), batches)
	}
	if len(issues) != 2 {
		t.Errorf("issues = %d, want results of both batches", len(issues))
	}
}
