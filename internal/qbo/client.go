// Package qbo is a small client for the QuickBooks Online Accounting API:
// OAuth 2.0 tokens kept in a local file, the query endpoint, and invoice
// creation. It covers what the MCP server needs and nothing more.
package qbo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Endpoints from Intuit's OpenID discovery document
// (developer.api.intuit.com/.well-known/openid_sandbox_configuration).
const (
	AuthorizeURL    = "https://appcenter.intuit.com/connect/oauth2"
	TokenURL        = "https://oauth.platform.intuit.com/oauth2/v1/tokens/bearer"
	AccountingScope = "com.intuit.quickbooks.accounting"

	sandboxBase    = "https://sandbox-quickbooks.api.intuit.com"
	productionBase = "https://quickbooks.api.intuit.com"

	// MinorVersion pins the response shape. Intuit retired minor versions
	// 1 to 74, so 75 is the floor.
	MinorVersion = "75"
)

// Config comes from the environment so no secret lives in the code.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Production   bool
	TokenFile    string
}

func ConfigFromEnv() (Config, error) {
	c := Config{
		ClientID:     os.Getenv("QBO_CLIENT_ID"),
		ClientSecret: os.Getenv("QBO_CLIENT_SECRET"),
		RedirectURI:  envOr("QBO_REDIRECT_URI", "http://localhost:8765/callback"),
		Production:   os.Getenv("QBO_ENV") == "production",
		TokenFile:    envOr("QBO_TOKEN_FILE", "tokens.json"),
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return c, errors.New("QBO_CLIENT_ID and QBO_CLIENT_SECRET must be set (see .env.example)")
	}
	return c, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (c Config) BaseURL() string {
	if c.Production {
		return productionBase
	}
	return sandboxBase
}

// Tokens is what we persist after the OAuth handshake. Intuit rotates the
// refresh token, so every refresh rewrites the file.
type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	RealmID      string    `json:"realm_id"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// ExchangeCode trades the authorization code from the callback for tokens.
func (c Config) ExchangeCode(ctx context.Context, code, realmID string) (Tokens, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {c.RedirectURI},
	}
	tr, err := c.postToken(ctx, form)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		RealmID:      realmID,
	}, nil
}

func (c Config) postToken(ctx context.Context, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.SetBasicAuth(c.ClientID, c.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return tokenResponse{}, fmt.Errorf("token endpoint: %s: %s", resp.Status, body)
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return tokenResponse{}, err
	}
	return tr, nil
}

func SaveTokens(path string, t Tokens) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func LoadTokens(path string) (Tokens, error) {
	var t Tokens
	b, err := os.ReadFile(path)
	if err != nil {
		return t, fmt.Errorf("no tokens yet, run qbo-connect first: %w", err)
	}
	return t, json.Unmarshal(b, &t)
}

// Client calls the Accounting API for one company (realm).
type Client struct {
	cfg    Config
	mu     sync.Mutex
	tokens Tokens
}

func NewClient(cfg Config) (*Client, error) {
	t, err := LoadTokens(cfg.TokenFile)
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, tokens: t}, nil
}

// accessToken refreshes a minute before expiry and persists the rotated
// refresh token.
func (c *Client) accessToken(ctx context.Context) (string, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Until(c.tokens.ExpiresAt) > time.Minute {
		return c.tokens.AccessToken, c.tokens.RealmID, nil
	}
	tr, err := c.cfg.postToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.tokens.RefreshToken},
	})
	if err != nil {
		return "", "", fmt.Errorf("refresh: %w", err)
	}
	c.tokens.AccessToken = tr.AccessToken
	c.tokens.RefreshToken = tr.RefreshToken
	c.tokens.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	if err := SaveTokens(c.cfg.TokenFile, c.tokens); err != nil {
		return "", "", fmt.Errorf("save rotated tokens: %w", err)
	}
	return c.tokens.AccessToken, c.tokens.RealmID, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	token, realm, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("minorversion", MinorVersion)
	u := fmt.Sprintf("%s/v3/company/%s/%s?%s", c.cfg.BaseURL(), realm, path, query.Encode())

	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		// Intuit puts the useful part in Fault.Error[].Detail; keep the
		// intuit_tid header so a support ticket can reference the call.
		return fmt.Errorf("quickbooks %s %s: %s (intuit_tid %s): %s",
			method, path, resp.Status, resp.Header.Get("intuit_tid"), raw)
	}
	return json.Unmarshal(raw, out)
}

// Query runs a QuickBooks query ("select * from Customer where ...").
func (c *Client) Query(ctx context.Context, q string, out any) error {
	return c.do(ctx, http.MethodGet, "query", url.Values{"query": {q}}, nil, out)
}

// Quote escapes a value for a QuickBooks query string literal.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}
