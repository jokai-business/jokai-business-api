package settings

import "ai-business-api/internal/user"

// IntegrationConfig contains only validated startup capabilities, never credentials.
type IntegrationConfig struct {
	StorageBackend                  string
	AI, Gmail, Email, OCR, KhmerOCR bool
}
type IntegrationStatus struct {
	StorageBackend string `json:"storageBackend"`
	AI             string `json:"ai"`
	Gmail          string `json:"gmail"`
	Email          string `json:"email"`
	OCR            string `json:"ocr"`
	KhmerOCR       string `json:"khmerOcr"`
	Drive          string `json:"drive"`
	Telegram       string `json:"telegram"`
}

// GetIntegrations does not probe providers or expose any user's personal connections.
func (s *Service) GetIntegrations(a user.Context) (IntegrationStatus, error) {
	if !a.Allows("settings.manage") {
		return IntegrationStatus{}, user.ErrForbidden
	}
	c := s.Integrations
	storage := "unavailable"
	if c.StorageBackend == "local" || c.StorageBackend == "s3" {
		storage = c.StorageBackend
	}
	status := func(enabled bool) string {
		if enabled {
			return "configured"
		}
		return "not_configured"
	}
	return IntegrationStatus{StorageBackend: storage, AI: status(c.AI), Gmail: status(c.Gmail), Email: status(c.Email), OCR: status(c.OCR), KhmerOCR: status(c.KhmerOCR), Drive: "unavailable", Telegram: "unavailable"}, nil
}
