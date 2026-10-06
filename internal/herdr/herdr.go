// Package herdr calls the herdr CLI, which is the plugin API, and decodes its
// JSON responses.
package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Workspace is a herdr workspace record.
type Workspace struct {
	ID       string             `json:"workspace_id"`
	Label    string             `json:"label"`
	Worktree *WorkspaceWorktree `json:"worktree"`
}

// WorkspaceWorktree is the worktree provenance attached to a workspace.
type WorkspaceWorktree struct {
	RepoKey          string `json:"repo_key"`
	CheckoutPath     string `json:"checkout_path"`
	IsLinkedWorktree bool   `json:"is_linked_worktree"`
}

// Worktree is a git checkout of a repository as reported by herdr.
type Worktree struct {
	Path             string `json:"path"`
	Branch           string `json:"branch"`
	IsDetached       bool   `json:"is_detached"`
	IsLinkedWorktree bool   `json:"is_linked_worktree"`
	OpenWorkspaceID  string `json:"open_workspace_id"`
}

// Error is a herdr API error response.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return "herdr: " + e.Code + ": " + e.Message
}

// Client runs herdr commands with the binary at Bin, normally the
// HERDR_BIN_PATH that herdr injects into plugin commands.
type Client struct {
	Bin string
}

// WorkspaceGet returns one workspace.
func (c *Client) WorkspaceGet(ctx context.Context, id string) (Workspace, error) {
	var res struct {
		Workspace Workspace `json:"workspace"`
	}
	err := c.run(ctx, &res, "workspace", "get", id)
	return res.Workspace, err
}

// WorkspaceList returns all workspaces of the session.
func (c *Client) WorkspaceList(ctx context.Context) ([]Workspace, error) {
	var res struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	err := c.run(ctx, &res, "workspace", "list")
	return res.Workspaces, err
}

// WorkspaceRename sets a workspace's label.
func (c *Client) WorkspaceRename(ctx context.Context, id, label string) error {
	return c.run(ctx, nil, "workspace", "rename", id, label)
}

// WorktreeList returns all checkouts of the repository that workspaceID
// belongs to.
func (c *Client) WorktreeList(ctx context.Context, workspaceID string) ([]Worktree, error) {
	var res struct {
		Worktrees []Worktree `json:"worktrees"`
	}
	err := c.run(ctx, &res, "worktree", "list", "--workspace", workspaceID)
	return res.Worktrees, err
}

// Notify shows a herdr notification. herdr silently skips it when no client
// is attached, which is not an error.
func (c *Client) Notify(ctx context.Context, title, body string) error {
	return c.run(ctx, nil, "notification", "show", title, "--body", body)
}

// OpenPluginPane opens a manifest pane entrypoint of a plugin.
func (c *Client) OpenPluginPane(ctx context.Context, pluginID, entrypoint string) error {
	return c.run(ctx, nil, "plugin", "pane", "open", "--plugin", pluginID, "--entrypoint", entrypoint, "--focus")
}

func (c *Client) run(ctx context.Context, result any, args ...string) error {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	// Server errors are JSON on stderr; successes are JSON on stdout.
	if runErr != nil {
		if json.Unmarshal(stderr.Bytes(), &resp) == nil && resp.Error != nil {
			return resp.Error
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return fmt.Errorf("herdr %s: %w: %s", strings.Join(args, " "), runErr, strings.TrimSpace(stderr.String()))
		}
		return fmt.Errorf("herdr %s: %w", strings.Join(args, " "), runErr)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return fmt.Errorf("herdr %s: decode response: %w", strings.Join(args, " "), err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	return json.Unmarshal(resp.Result, result)
}
