package config

import (
	"errors"
	"os"
)

type Config struct {
	Address, DatabaseURL    string
	Issuer, JWKSURL, Origin string
}

func Load() (Config, error) {
	c := Config{Address: os.Getenv("HTTP_ADDR"), DatabaseURL: os.Getenv("DATABASE_URL")}
	c.Issuer = os.Getenv("OIDC_ISSUER")
	c.JWKSURL = os.Getenv("OIDC_JWKS_URL")
	c.Origin = os.Getenv("APP_URL")
	if c.Address == "" {
		c.Address = ":8080"
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	return c, nil
}
