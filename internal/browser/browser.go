// Package browser opens URLs in the user's browser on macOS, Linux and WSL.
package browser

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const wslPowerShell = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"

// Open asks the platform to open url. The caller should also print the URL
// because no browser may be available.
func Open(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name, args := command(url)
	return exec.CommandContext(ctx, name, args...).Run()
}

func command(url string) (string, []string) {
	switch {
	case runtime.GOOS == "darwin":
		return "open", []string{url}
	case isWSL():
		// WSL usually has neither wslview nor xdg-open. cmd.exe "start" would
		// split the URL at "&", so use PowerShell with a single-quoted string.
		ps := "powershell.exe"
		if _, err := exec.LookPath(ps); err != nil {
			ps = wslPowerShell
		}
		quoted := "'" + strings.ReplaceAll(url, "'", "''") + "'"
		return ps, []string{"-NoProfile", "-NonInteractive", "-Command", "Start-Process " + quoted}
	default:
		return "xdg-open", []string{url}
	}
}

func isWSL() bool {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}
