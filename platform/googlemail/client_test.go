package googlemail

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestConfigAndVault(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	c, e := Read(get)
	if e != nil || c.Enabled() {
		t.Fatal("disabled default", e)
	}
	env["GMAIL_CLIENT_ID"] = "client"
	if _, e = Read(get); e == nil {
		t.Fatal("partial config accepted")
	}
	env["GMAIL_CLIENT_SECRET"] = "secret"
	env["GMAIL_TOKEN_KEY"] = base64.StdEncoding.EncodeToString(make([]byte, 32))
	env["APP_URL"] = "https://localhost:8443"
	c, e = Read(get)
	if e != nil || c.RedirectURI != "https://localhost:8443/api/v1/gmail/callback" {
		t.Fatal(c.RedirectURI, e)
	}
	for _, bad := range []string{"http://localhost:8443", "https://user:pass@example.com", "https://example.com/path", "https://example.com?query=yes"} {
		env["APP_URL"] = bad
		if _, e = Read(get); e == nil {
			t.Fatal("invalid origin", bad)
		}
	}
	v, _ := NewVault(c.Key)
	encrypted, _ := v.Seal("private-refresh-token", "org:user:token")
	other, _ := v.Seal("private-refresh-token", "org:user:token")
	if string(encrypted) == string(other) || strings.Contains(string(encrypted), "private-refresh-token") {
		t.Fatal("unsafe ciphertext")
	}
	if plain, e := v.Open(encrypted, "org:user:token"); e != nil || plain != "private-refresh-token" {
		t.Fatal("round trip", e)
	}
	for _, owner := range []string{"other:user:token", "org:other:token", "org:user:state"} {
		if _, e = v.Open(encrypted, owner); e == nil {
			t.Fatal("accepted another owner")
		}
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, e = v.Open(encrypted, "org:user:token"); e == nil {
		t.Fatal("accepted tampering")
	}
}
func TestOAuthProtocol(t *testing.T) {
	var mode string
	var revoked bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			r.ParseForm()
			if r.Form.Get("code_verifier") != "verifier" || r.Form.Get("client_secret") != "secret" || r.Form.Get("grant_type") != "authorization_code" {
				t.Error("invalid exchange")
			}
			switch mode {
			case "redirect":
				http.Redirect(w, r, "/leak", 302)
				return
			case "oversized":
				fmt.Fprint(w, strings.Repeat("x", 65537))
				return
			case "private-error":
				http.Error(w, "sensitive provider content", 400)
				return
			case "missing-refresh":
				fmt.Fprintf(w, `{"access_token":"access","token_type":"Bearer","scope":%q}`, Scope)
				return
			case "missing-scope":
				fmt.Fprint(w, `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","scope":"email"}`)
				return
			}
			fmt.Fprintf(w, `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","scope":%q}`, Scope)
		case "/profile":
			if r.Header.Get("Authorization") != "Bearer access" {
				t.Error("missing bearer")
			}
			fmt.Fprint(w, `{"emailAddress":"mailbox@example.test"}`)
		case "/revoke":
			r.ParseForm()
			revoked = r.Form.Get("token") == "refresh"
			fmt.Fprint(w, `{}`)
		default:
			t.Error("followed redirect")
			http.Error(w, "unexpected", 400)
		}
	}))
	defer server.Close()
	c := New(Config{ClientID: "client", ClientSecret: "secret", RedirectURI: "https://app.test/api/v1/gmail/callback"})
	c.tokenURL = server.URL + "/token"
	c.profileURL = server.URL + "/profile"
	c.revokeURL = server.URL + "/revoke"
	u, _ := url.Parse(c.AuthorizationURL("state", "verifier"))
	q := u.Query()
	if q.Get("scope") != Scope || q.Get("state") != "state" || q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" || q.Get("client_secret") != "" {
		t.Fatal("unsafe consent URL")
	}
	g, e := c.Exchange(context.Background(), "code", "verifier")
	if e != nil || g.Email != "mailbox@example.test" || g.RefreshToken != "refresh" {
		t.Fatal(g, e)
	}
	if c.Revoke(context.Background(), g.RefreshToken) != nil || !revoked {
		t.Fatal("revoke failed")
	}
	for _, mode = range []string{"redirect", "oversized", "private-error", "missing-refresh", "missing-scope"} {
		if _, e = c.Exchange(context.Background(), "code", "verifier"); e != ErrProvider {
			t.Fatal("provider failure not sanitized", mode, e)
		}
	}
}
