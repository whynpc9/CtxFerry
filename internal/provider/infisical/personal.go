package infisical

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

// Adopt an existing CLI session after the user has signed in through Infisical.
// The empty working directory prevents a checkout from selecting CLI settings.
func cliPersonalToken(ctx context.Context, profile string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "ctxferry-infisical-")
	if err != nil {
		return nil, provider.ErrAuthenticationRequired
	}
	defer os.Remove(dir)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	args := []string{"user", "get", "token", "--plain", "--silent", "--telemetry=false", "--domain=" + cloud}
	if profile != "default" {
		args = append(args, "--profile="+profile)
	}
	cmd := exec.CommandContext(ctx, "infisical", args...)
	cmd.Dir = dir
	cmd.Stderr = io.Discard
	cmd.Env = personalEnvironment()
	output, err := cmd.Output()
	defer clear(output)
	token := bytes.TrimSpace(output)
	if err != nil || tokenExpiry(token, personal) == nil {
		return nil, provider.ErrAuthenticationRequired
	}
	return bytes.Clone(token), nil
}

func personalEnvironment() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA",
			"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS",
			"TMPDIR", "TMP", "TEMP":
			env = append(env, entry)
		}
	}
	return env
}
