// Package infisical resolves explicit Infisical US Cloud references without a local secret cache.
package infisical

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

const (
	Name          = "infisical"
	personal      = "personal"
	universalAuth = "universal-auth"

	cloud          = "https://app.infisical.com"
	personalStatus = "/api/v2/users/me"
	machineStatus  = "/api/v1/identities/details"
	machineLogin   = "/api/v1/auth/universal-auth/login"
	secrets        = "/api/v4/secrets"
)

type adapter struct {
	baseURL            string
	client             *http.Client
	personalToken      func(context.Context, string) ([]byte, error)
	machineCredentials func(string) (clientID, clientSecret string)
	now                func() time.Time
}

func New() provider.Adapter {
	return newAdapter(cloud)
}

func newAdapter(baseURL string) *adapter {
	return &adapter{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		personalToken: cliPersonalToken,
		now:           time.Now,
		machineCredentials: func(profile string) (string, string) {
			if os.Getenv("CTXFERRY_INFISICAL_PROFILE") != profile {
				return "", ""
			}
			return os.Getenv("INFISICAL_CLIENT_ID"), os.Getenv("INFISICAL_CLIENT_SECRET")
		},
	}
}

// InjectedSession reads an access token provisioned by a headless host's
// existing credential mechanism. The scope must match this workspace, device
// and machine profile before the token can be used.
func InjectedSession(key provider.SessionKey) []byte {
	if !matchesInjectionScope(key) {
		return nil
	}
	return []byte(os.Getenv("CTXFERRY_INFISICAL_ACCESS_TOKEN"))
}

// HasInjectedSession checks the exact scope and only the presence of the
// access token environment entry. Session contents and validity are unknown.
func HasInjectedSession(key provider.SessionKey) bool {
	if !matchesInjectionScope(key) {
		return false
	}
	_, present := os.LookupEnv("CTXFERRY_INFISICAL_ACCESS_TOKEN")
	return present
}

func matchesInjectionScope(key provider.SessionKey) bool {
	return key.Profile.Provider == Name && key.Profile.Method == universalAuth &&
		key.WorkspaceID != "" && key.DeviceID != "" && key.Profile.Name != "" &&
		key.WorkspaceID == os.Getenv("CTXFERRY_INFISICAL_WORKSPACE_ID") &&
		key.DeviceID == os.Getenv("CTXFERRY_INFISICAL_DEVICE_ID") &&
		key.Profile.Name == os.Getenv("CTXFERRY_INFISICAL_PROFILE")
}

func (*adapter) Capabilities() []provider.MethodCapabilities {
	return []provider.MethodCapabilities{
		{Method: personal, SessionExpiry: true},
		{Method: universalAuth, MachineAuth: true, SessionExpiry: true},
	}
}

func (a *adapter) Login(ctx context.Context, p provider.Profile) ([]byte, error) {
	if p.Provider == Name && p.Name != "" {
		switch p.Method {
		case personal:
			return a.personalToken(ctx, p.Name)
		case universalAuth:
			return a.loginMachine(ctx, p.Name)
		}
	}
	return nil, provider.ErrAuthenticationRequired
}

func (a *adapter) loginMachine(ctx context.Context, profile string) ([]byte, error) {
	id, secret := a.machineCredentials(profile)
	if id == "" || secret == "" {
		return nil, provider.ErrAuthenticationRequired
	}
	payload, err := json.Marshal(struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}{id, secret})
	if err != nil {
		return nil, provider.ErrAuthenticationRequired
	}
	defer clear(payload)
	var result struct {
		Token string `json:"accessToken"`
	}
	status, err := a.request(ctx, http.MethodPost, machineLogin, nil, payload, &result)
	if err != nil || status != http.StatusOK || tokenExpiry([]byte(result.Token), universalAuth) == nil {
		return nil, provider.ErrAuthenticationRequired
	}
	return []byte(result.Token), nil
}

func statusRoute(p provider.Profile) string {
	if p.Provider == Name && p.Name != "" {
		switch p.Method {
		case personal:
			return personalStatus
		case universalAuth:
			return machineStatus
		}
	}
	return ""
}

// JWT expiry is an early rejection signal. Only Infisical can confirm the
// token's signature, authentication method and current session validity.
func tokenExpiry(session []byte, method string) *time.Time {
	if len(session) == 0 || len(session) > 16<<10 {
		return nil
	}
	for _, c := range session {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return nil
		}
	}
	parts := bytes.Split(session, []byte("."))
	if len(parts) != 3 || len(parts[0]) == 0 || len(parts[2]) == 0 {
		return nil
	}
	claims := make([]byte, base64.RawURLEncoding.DecodedLen(len(parts[1])))
	defer clear(claims)
	n, err := base64.RawURLEncoding.Decode(claims, parts[1])
	if err != nil {
		return nil
	}
	var payload struct {
		Exp  int64  `json:"exp"`
		Type string `json:"authTokenType"`
	}
	if json.Unmarshal(claims[:n], &payload) != nil || payload.Exp <= 0 {
		return nil
	}
	if method == personal && payload.Type != "accessToken" ||
		method == universalAuth && payload.Type != "identityAccessToken" {
		return nil
	}
	expiry := time.Unix(payload.Exp, 0).UTC()
	if expiry.Year() > 9999 {
		return nil
	}
	return &expiry
}

func (a *adapter) Authentication(ctx context.Context, p provider.Profile, session []byte) (provider.Authentication, error) {
	route, expiry := statusRoute(p), tokenExpiry(session, p.Method)
	if route == "" || expiry == nil {
		return provider.Authentication{State: provider.SignedOut}, nil
	}
	if !expiry.After(a.now()) {
		return provider.Authentication{State: provider.Expired, ExpiresAt: expiry}, nil
	}
	var result struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Identity struct {
			Organization struct {
				ID string `json:"id"`
			} `json:"organization"`
		} `json:"identityDetails"`
	}
	status, err := a.request(ctx, http.MethodGet, route, session, nil, &result)
	if err == nil {
		switch status {
		case http.StatusOK:
			if p.Method == personal && result.User.ID != "" ||
				p.Method == universalAuth && result.Identity.Organization.ID != "" {
				return provider.Authentication{State: provider.Authenticated, ExpiresAt: expiry}, nil
			}
		case http.StatusUnauthorized:
			return provider.Authentication{State: provider.SignedOut}, nil
		case http.StatusForbidden:
			return provider.Authentication{State: provider.Locked, ExpiresAt: expiry}, nil
		}
	}
	return provider.Authentication{}, errors.New("could not verify the Infisical session online")
}

func (a *adapter) Resolve(ctx context.Context, p provider.Profile, session []byte, locator provider.Locator) ([]byte, error) {
	expiry := tokenExpiry(session, p.Method)
	if statusRoute(p) == "" || expiry == nil || !expiry.After(a.now()) {
		return nil, provider.ErrAuthenticationRequired
	}
	route := secretRoute(locator)
	if route == "" {
		return nil, provider.ErrSecretUnavailable
	}
	var result struct {
		Secret struct {
			ID          string  `json:"id"`
			Workspace   string  `json:"workspace"`
			Environment string  `json:"environment"`
			Path        string  `json:"secretPath"`
			Key         string  `json:"secretKey"`
			Type        string  `json:"type"`
			Value       *string `json:"secretValue"`
			Hidden      *bool   `json:"secretValueHidden"`
		} `json:"secret"`
	}
	status, err := a.request(ctx, http.MethodGet, route, session, nil, &result)
	if status == http.StatusUnauthorized {
		return nil, provider.ErrAuthenticationRequired
	}
	s := result.Secret
	if err != nil || status != http.StatusOK || s.Value == nil || s.Hidden == nil || *s.Hidden {
		return nil, provider.ErrSecretUnavailable
	}
	if locator.EntryID != "" {
		if s.ID != locator.EntryID {
			return nil, provider.ErrSecretUnavailable
		}
	} else if s.Key != locator.Key || s.Workspace != locator.ProjectID || s.Environment != locator.Environment ||
		s.Path != locator.Path || s.Type != "shared" {
		return nil, provider.ErrSecretUnavailable
	}
	return []byte(*s.Value), nil
}

func explicitSegment(value string) bool {
	return value != "" && value != "." && value != ".." &&
		value == strings.TrimSpace(value) && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func secretRoute(l provider.Locator) string {
	if l.EntryID != "" {
		if l != (provider.Locator{EntryID: l.EntryID}) || !explicitSegment(l.EntryID) {
			return ""
		}
		return secrets + "/id/" + url.PathEscape(l.EntryID) + "?viewSecretValue=true"
	}
	if !explicitSegment(l.Key) || !explicitSegment(l.ProjectID) || !explicitSegment(l.Environment) ||
		!strings.HasPrefix(l.Path, "/") || path.Clean(l.Path) != l.Path ||
		l.Path != strings.TrimSpace(l.Path) || strings.ContainsAny(l.Path, "\\\x00\r\n") {
		return ""
	}
	query := url.Values{
		"projectId": {l.ProjectID}, "environment": {l.Environment}, "secretPath": {l.Path},
		"type": {"shared"}, "includeImports": {"false"}, "expandSecretReferences": {"false"}, "viewSecretValue": {"true"},
	}
	return secrets + "/" + url.PathEscape(l.Key) + "?" + query.Encode()
}

var errResponse = errors.New("invalid Infisical response")

func (a *adapter) request(ctx context.Context, method, route string, session, payload []byte, result any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+route, bytes.NewReader(payload))
	if err != nil {
		return 0, errResponse
	}
	if len(session) > 0 {
		req.Header.Set("Authorization", "Bearer "+string(session))
	}
	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, errResponse
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	const maxResponse = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	defer clear(body)
	if err != nil || len(body) > maxResponse || json.Unmarshal(body, result) != nil {
		return resp.StatusCode, errResponse
	}
	return resp.StatusCode, nil
}
