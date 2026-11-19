package infisical

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

var _ provider.Importer = (*adapter)(nil)

// Import uses the v4 create endpoint. That endpoint rejects existing shared
// entries and may return an approval object instead of a created secret.
func (a *adapter) Import(ctx context.Context, p provider.Profile, session []byte, l provider.Locator, value []byte) error {
	if p.Method != universalAuth || statusRoute(p) == "" {
		return provider.ErrImportRejected
	}
	expiry := tokenExpiry(session, p.Method)
	if expiry == nil || !expiry.After(a.now()) {
		return provider.ErrAuthenticationRequired
	}
	// The API trims values and can interpret secret references. Reject inputs
	// whose literal meaning cannot be preserved by this import operation.
	text := string(value)
	if l.EntryID != "" || secretRoute(l) == "" || !utf8.Valid(value) ||
		text != strings.TrimFunc(text, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' }) ||
		strings.Contains(text, "${") || strings.ContainsAny(text, "\x00\r\n") {
		return provider.ErrImportRejected
	}
	payload, err := json.Marshal(struct {
		ProjectID   string `json:"projectId"`
		Environment string `json:"environment"`
		Path        string `json:"secretPath"`
		Type        string `json:"type"`
		Value       string `json:"secretValue"`
	}{l.ProjectID, l.Environment, l.Path, "shared", text})
	if err != nil {
		return provider.ErrImportRejected
	}
	defer clear(payload)
	resp, err := a.send(ctx, http.MethodPost, secrets+"/"+url.PathEscape(l.Key), session, payload)
	if err != nil {
		return provider.ErrImportUnknown
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var result struct {
			Secret struct {
				ID          string `json:"id"`
				Key         string `json:"secretKey"`
				Workspace   string `json:"workspace"`
				Environment string `json:"environment"`
				Type        string `json:"type"`
			} `json:"secret"`
			Approval *struct{} `json:"approval"`
		}
		if decodeResponse(resp, &result) != nil {
			return provider.ErrImportUnknown
		}
		if result.Approval != nil {
			return provider.ErrImportPending
		}
		s := result.Secret
		if s.ID != "" && s.Key == l.Key && s.Workspace == l.ProjectID && s.Environment == l.Environment && s.Type == "shared" {
			return nil
		}
	case http.StatusBadRequest:
		var result struct {
			Message string `json:"message"`
		}
		collision := "Secret '" + l.Key + "' already exists in path '" + l.Path + "' of environment '" + l.Environment + "'"
		if decodeResponse(resp, &result) == nil && result.Message == collision {
			return provider.ErrImportCollision
		}
		return provider.ErrImportRejected
	case http.StatusConflict:
		return provider.ErrImportCollision
	case http.StatusUnauthorized:
		return provider.ErrAuthenticationRequired
	case http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusTooManyRequests:
		return provider.ErrImportRejected
	}
	return provider.ErrImportUnknown
}
