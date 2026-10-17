package auth

import (
	"context"
	"errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"time"
)

type Identity struct{ Subject, SessionID string }
type Verifier interface {
	Verify(context.Context, string) (Identity, error)
}
type JWTVerifier struct{ verifier *oidc.IDTokenVerifier }

func New(ctx context.Context, issuer, audience, jwks string) *JWTVerifier {
	return &JWTVerifier{oidc.NewVerifier(issuer, oidc.NewRemoteKeySet(ctx, jwks), &oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{"RS256"}})}
}
func (v *JWTVerifier) Verify(ctx context.Context, raw string) (Identity, error) {
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, errors.New("invalid token")
	}
	var claims struct {
		NBF             int64  `json:"nbf"`
		Type            string `json:"typ"`
		AuthorizedParty string `json:"azp"`
		EmailVerified   bool   `json:"email_verified"`
		SessionID       string `json:"sid"`
	}
	if err = token.Claims(&claims); err != nil {
		return Identity{}, errors.New("invalid claims")
	}
	if token.Subject == "" || claims.SessionID == "" || claims.Type != "Bearer" || claims.AuthorizedParty != "ai-business-web" || !claims.EmailVerified || claims.NBF > time.Now().Unix() {
		return Identity{}, errors.New("invalid access token")
	}
	return Identity{Subject: token.Subject, SessionID: claims.SessionID}, nil
}
