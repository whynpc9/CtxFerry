// Package bitwarden resolves exact Bitwarden Password Manager references using bw.
package bitwarden

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/provider"
)

const (
	Name   = "bitwarden"
	Method = "personal"

	profileEnv = "CTXFERRY_BITWARDEN_PROFILE"
	sessionEnv = "BW_SESSION"
	dataDirEnv = "BITWARDENCLI_APPDATA_DIR"
)

type adapter struct {
	run func(context.Context, []byte, ...string) ([]byte, error)
}

func New() provider.Adapter {
	return &adapter{run: runCLI}
}

// RuntimeSession accepts BW_SESSION only when the launcher explicitly scopes
// it to the same workspace, device and provider profile as the binding.
var RuntimeSession = provider.EnvironmentSession{
	Provider: Name, Method: Method, ScopePrefix: "CTXFERRY_BITWARDEN_", Variable: sessionEnv,
}

func (*adapter) Capabilities() []provider.MethodCapabilities {
	return []provider.MethodCapabilities{{Method: Method, OfflineRead: true}}
}

func selectedProfile(p provider.Profile) bool {
	name := os.Getenv(profileEnv)
	if name == "" {
		name = "default"
	}
	return p.Provider == Name && p.Method == Method && p.Name == name
}

func (*adapter) Login(_ context.Context, p provider.Profile) ([]byte, error) {
	// The caller validates the adopted key before saving it. bw owns sign-in
	// and unlock. CtxFerry never receives a master password.
	if selectedProfile(p) {
		if session := os.Getenv(sessionEnv); session != "" {
			return []byte(session), nil
		}
	}
	return nil, provider.ErrNoSession
}

func (a *adapter) Authentication(ctx context.Context, p provider.Profile, session []byte) (provider.Authentication, error) {
	if !selectedProfile(p) {
		return provider.Authentication{State: provider.SignedOut}, nil
	}
	output, err := a.run(ctx, session, "status", "--nointeraction")
	defer clear(output)
	var result struct{ Status string }
	if err != nil || json.Unmarshal(output, &result) != nil {
		return provider.Authentication{}, errors.New("could not check Bitwarden CLI status")
	}
	auth := provider.Authentication{
		LoggedIn: new(bool), Unlocked: new(bool), SessionValidity: provider.SessionAbsent,
	}
	if len(session) != 0 {
		auth.SessionValidity = provider.SessionInvalid
	}
	switch result.Status {
	case "unauthenticated":
		auth.State = provider.SignedOut
	case "locked", "unlocked":
		*auth.LoggedIn = true
		*auth.Unlocked = result.Status == "unlocked"
		auth.State = provider.Locked
		if len(session) != 0 && *auth.Unlocked {
			auth.State, auth.SessionValidity = provider.Authenticated, provider.SessionValid
		}
	default:
		return provider.Authentication{}, errors.New("unknown Bitwarden CLI status")
	}
	return auth, nil
}

// ValidLocator requires an item UUID. bw interprets anything else as a vault
// search, which can match an unrelated item by name, username or URI.
func ValidLocator(ref provider.Locator) bool {
	id, err := uuid.Parse(ref.EntryID)
	return err == nil && id != uuid.Nil && strings.EqualFold(id.String(), ref.EntryID) &&
		ref == (provider.Locator{EntryID: ref.EntryID})
}

func (a *adapter) Resolve(ctx context.Context, p provider.Profile, session []byte, ref provider.Locator) ([]byte, error) {
	if !selectedProfile(p) || len(session) == 0 {
		return nil, provider.ErrAuthenticationRequired
	}
	if !ValidLocator(ref) {
		return nil, provider.ErrSecretUnavailable
	}
	output, err := a.run(ctx, session, "get", "password", strings.ToLower(ref.EntryID), "--response", "--nointeraction")
	defer clear(output)
	var result struct {
		Success bool
		Message string
		Data    struct {
			Object string
			Data   string
		}
	}
	if json.Unmarshal(output, &result) != nil {
		return nil, provider.ErrSecretUnavailable
	}
	if !result.Success && (result.Message == "Vault is locked." || result.Message == "You are not logged in.") {
		return nil, provider.ErrAuthenticationRequired
	}
	if err != nil || !result.Success || result.Data.Object != "string" || strings.TrimSpace(result.Data.Data) == "" {
		return nil, provider.ErrSecretUnavailable
	}
	return []byte(result.Data.Data), nil
}

func runCLI(ctx context.Context, session []byte, args ...string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "ctxferry-bitwarden-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(dir)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bw", args...)
	cmd.Dir = dir
	cmd.Stderr = io.Discard
	cmd.Env = provider.CLIEnvironment()
	if dataDir := os.Getenv(dataDirEnv); dataDir != "" {
		if !filepath.IsAbs(dataDir) {
			return nil, errors.New("Bitwarden CLI data directory must be absolute")
		}
		cmd.Env = append(cmd.Env, dataDirEnv+"="+dataDir)
	}
	if len(session) != 0 {
		if bytes.IndexByte(session, 0) >= 0 {
			return nil, provider.ErrAuthenticationRequired
		}
		cmd.Env = append(cmd.Env, sessionEnv+"="+string(session))
	}
	return cmd.Output()
}
