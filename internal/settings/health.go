package settings

import (
	"ai-business-api/internal/user"
	"context"
	"sync"
	"time"
)

type HealthProbe interface{ CheckHealth(context.Context) error }
type DatabaseProbe interface{ Ping(context.Context) error }
type HealthStatus struct {
	CheckedAt time.Time `json:"checkedAt"`
	Database  string    `json:"database"`
	Storage   string    `json:"storage"`
	OCR       string    `json:"ocr"`
	KhmerOCR  string    `json:"khmerOcr"`
}

// HealthMonitor caches a deployment-wide, content-free snapshot to bound probe load.
// Probe implementations must honor context cancellation.
type HealthMonitor struct {
	Database               DatabaseProbe
	Storage, OCR, KhmerOCR HealthProbe
	mu                     sync.Mutex
	cached                 HealthStatus
}

func (h *HealthMonitor) Check(ctx context.Context, a user.Context) (HealthStatus, error) {
	if !a.Allows("settings.manage") {
		return HealthStatus{}, user.ErrForbidden
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.cached.CheckedAt.IsZero() && time.Since(h.cached.CheckedAt) < 15*time.Second {
		return h.cached, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result := HealthStatus{Database: "not_checked", Storage: "not_checked", OCR: "not_configured", KhmerOCR: "not_configured"}
	var wg sync.WaitGroup
	probe := func(check func(context.Context) error, dest *string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			*dest = "unavailable"
			if check(ctx) == nil {
				*dest = "reachable"
			}
		}()
	}
	if h.Database != nil {
		probe(h.Database.Ping, &result.Database)
	}
	if h.Storage != nil {
		probe(h.Storage.CheckHealth, &result.Storage)
	}
	if h.OCR != nil {
		probe(h.OCR.CheckHealth, &result.OCR)
	}
	if h.KhmerOCR != nil {
		probe(h.KhmerOCR.CheckHealth, &result.KhmerOCR)
	}
	wg.Wait()
	result.CheckedAt = time.Now().UTC()
	h.cached = result
	return result, nil
}
