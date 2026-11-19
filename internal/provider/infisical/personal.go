package infisical

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
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
	cmd.Env = provider.CLIEnvironment()
	output, err := cmd.Output()
	defer clear(output)
	token := bytes.TrimSpace(output)
	if err != nil || tokenExpiry(token, personal) == nil {
		return nil, provider.ErrAuthenticationRequired
	}
	return bytes.Clone(token), nil
}
