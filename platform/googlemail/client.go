// Package googlemail implements the read-only Google authorization boundary.
package googlemail

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

const Scope = "https://www.googleapis.com/auth/gmail.readonly"

var ErrProvider = errors.New("Google authorization could not be completed")

type Config struct {
	ClientID, ClientSecret, RedirectURI string
	Key                                 []byte
}

func Read(get func(string) string) (Config, error) {
	c := Config{ClientID: get("GMAIL_CLIENT_ID"), ClientSecret: get("GMAIL_CLIENT_SECRET")}
	key := get("GMAIL_TOKEN_KEY")
	if c.ClientID == "" && c.ClientSecret == "" && key == "" {
		return c, nil
	}
	var e error
	c.Key, e = base64.StdEncoding.DecodeString(key)
	u, ue := url.Parse(get("APP_URL"))
	if e != nil || len(c.Key) != 32 || c.ClientID == "" || c.ClientSecret == "" || ue != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return Config{}, errors.New("Gmail requires a client ID, client secret, base64 32-byte token key and HTTPS APP_URL origin")
	}
	c.RedirectURI = strings.TrimSuffix(u.String(), "/") + "/api/v1/gmail/callback"
	return c, nil
}
func (c Config) Enabled() bool { return c.ClientID != "" }

type Vault struct{ aead cipher.AEAD }

func NewVault(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("Gmail token key must be 32 bytes")
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	return &Vault{a}, e
}
func (v *Vault) Seal(value, owner string) ([]byte, error) {
	n := make([]byte, v.aead.NonceSize())
	if _, e := rand.Read(n); e != nil {
		return nil, e
	}
	return v.aead.Seal(n, n, []byte(value), []byte(owner)), nil
}
func (v *Vault) Open(data []byte, owner string) (string, error) {
	n := v.aead.NonceSize()
	if len(data) < n {
		return "", ErrProvider
	}
	b, e := v.aead.Open(nil, data[:n], data[n:], []byte(owner))
	if e != nil {
		return "", ErrProvider
	}
	return string(b), nil
}

type Grant struct{ Email, RefreshToken string }
type Client struct {
	config                          Config
	http                            *http.Client
	tokenURL, profileURL, revokeURL string
}

func New(c Config) *Client {
	return &Client{c, &http.Client{Timeout: 7 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, "https://oauth2.googleapis.com/token", "https://gmail.googleapis.com/gmail/v1/users/me/profile", "https://oauth2.googleapis.com/revoke"}
}
func (c *Client) AuthorizationURL(state, verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {c.config.ClientID}, "redirect_uri": {c.config.RedirectURI}, "response_type": {"code"}, "scope": {Scope}, "access_type": {"offline"}, "prompt": {"consent select_account"}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(h[:])}, "code_challenge_method": {"S256"}}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
}
func (c *Client) request(ctx context.Context, method, endpoint string, form url.Values, token string, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r, e := http.NewRequestWithContext(ctx, method, endpoint, body)
	if e != nil {
		return ErrProvider
	}
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := c.http.Do(r)
	if e != nil {
		return ErrProvider
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if e != nil || len(data) > 65536 || resp.StatusCode != 200 {
		return ErrProvider
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return ErrProvider
	}
	return nil
}
func (c *Client) Exchange(ctx context.Context, code, verifier string) (Grant, error) {
	// A single deadline bounds both token exchange and mailbox identity lookup.
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	var tok struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Scope   string `json:"scope"`
		Type    string `json:"token_type"`
	}
	e := c.request(ctx, "POST", c.tokenURL, url.Values{"client_id": {c.config.ClientID}, "client_secret": {c.config.ClientSecret}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {c.config.RedirectURI}, "grant_type": {"authorization_code"}}, "", &tok)
	if e != nil {
		return Grant{}, e
	}
	hasScope := false
	for _, s := range strings.Fields(tok.Scope) {
		if s == Scope {
			hasScope = true
		}
	}
	if !hasScope || !strings.EqualFold(tok.Type, "Bearer") || tok.Access == "" || tok.Refresh == "" || len(tok.Refresh) > 8192 || strings.ContainsAny(tok.Access, "\r\n") {
		return Grant{}, ErrProvider
	}
	var profile struct {
		Email string `json:"emailAddress"`
	}
	if e = c.request(ctx, "GET", c.profileURL, nil, tok.Access, &profile); e != nil {
		return Grant{}, e
	}
	addr, e := mail.ParseAddress(profile.Email)
	if e != nil || addr.Address != profile.Email || len(profile.Email) > 254 {
		return Grant{}, ErrProvider
	}
	return Grant{profile.Email, tok.Refresh}, nil
}
func (c *Client) Revoke(ctx context.Context, token string) error {
	return c.request(ctx, "POST", c.revokeURL, url.Values{"token": {token}}, "", nil)
}
