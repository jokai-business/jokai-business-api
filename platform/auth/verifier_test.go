package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"github.com/go-jose/go-jose/v4"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestJWTValidation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
	}))
	defer jwks.Close()
	verifier := New(context.Background(), "https://issuer.example", "ai-business-api", jwks.URL)
	for _, tc := range []struct {
		name, field string
		value       any
		ok          bool
	}{
		{"valid", "", nil, true}, {"wrong issuer", "iss", "https://evil.example", false}, {"wrong audience", "aud", "other-api", false},
		{"expired", "exp", time.Now().Add(-time.Hour).Unix(), false}, {"not yet valid", "nbf", time.Now().Add(time.Hour).Unix(), false},
		{"unverified email", "email_verified", false, false}, {"ID token", "typ", "ID", false}, {"wrong client", "azp", "other-client", false}, {"missing subject", "sub", "", false}, {"missing session", "sid", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"iss": "https://issuer.example", "aud": "ai-business-api", "sub": "subject", "sid": "session", "exp": time.Now().Add(time.Hour).Unix(), "nbf": time.Now().Add(-time.Minute).Unix(), "typ": "Bearer", "azp": "ai-business-web", "email_verified": true}
			if tc.field != "" {
				claims[tc.field] = tc.value
			}
			payload, _ := json.Marshal(claims)
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "test"}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			signed, err := signer.Sign(payload)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := signed.CompactSerialize()
			_, err = verifier.Verify(context.Background(), raw)
			if (err == nil) != tc.ok {
				t.Fatalf("accepted=%v, want %v", err == nil, tc.ok)
			}
			if tc.ok {
				if _, err = verifier.Verify(context.Background(), raw+"tampered"); err == nil {
					t.Fatal("tampered token accepted")
				}
			}
		})
	}
}
