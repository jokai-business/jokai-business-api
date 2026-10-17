package settings

import (
	"ai-business-api/internal/user"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestIntegrationStatus(t *testing.T) {
	s := &Service{Integrations: IntegrationConfig{StorageBackend: "https://secret.invalid/bucket", AI: true, Gmail: true, KhmerOCR: true}}
	if _, err := s.GetIntegrations(user.Context{}); !errors.Is(err, user.ErrForbidden) {
		t.Fatal("missing administrator authorization", err)
	}
	admin := user.Context{Permissions: []string{"settings.manage"}}
	v, err := s.GetIntegrations(admin)
	if err != nil || v.StorageBackend != "unavailable" || v.AI != "configured" || v.Gmail != "configured" || v.Email != "not_configured" || v.OCR != "not_configured" || v.KhmerOCR != "configured" || v.Drive != "unavailable" || v.Telegram != "unavailable" {
		t.Fatalf("incorrect status: %+v %v", v, err)
	}
	data, _ := json.Marshal(v)
	if strings.Contains(string(data), "secret") {
		t.Fatal("exposed configuration")
	}
	for _, backend := range []string{"local", "s3"} {
		s.Integrations.StorageBackend = backend
		v, _ = s.GetIntegrations(admin)
		if v.StorageBackend != backend {
			t.Fatal(v)
		}
	}
}
