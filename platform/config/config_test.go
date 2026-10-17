package config

import "testing"

func TestRequiredDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing database accepted")
	}
}
func TestDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/app")
	t.Setenv("HTTP_ADDR", "")
	c, err := Load()
	if err != nil || c.Address != ":8080" {
		t.Fatal("invalid default")
	}
}
