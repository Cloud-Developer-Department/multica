package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// GET /api/dashboard/rates — realtime USD→CNY exchange rate for the personal
// dashboard's ¥ cost display (API contract §8 / data spec §4.4).
//
// Strategy: an external free FX endpoint (open.er-api.com by default, over-
// ridable via MULTICA_DASHBOARD_FX_URL) is the primary source; on any fetch
// failure the configured default (MULTICA_DASHBOARD_FX_DEFAULT_CNY, default
// 7.2) is returned with source="default". A short TTL cache (default 5 min,
// MULTICA_DASHBOARD_FX_TTL_SECONDS) keeps the external API from being
// hammered; the fallback value is also cached so an outage doesn't cause a
// fetch on every dashboard load.
// ---------------------------------------------------------------------------

// Default and env knobs.
const (
	dashboardFXDefaultURL = "https://open.er-api.com/v6/latest/USD"
	dashboardFXDefaultCNY = 7.2
	dashboardFXDefaultTTL = 5 * time.Minute

	envDashboardFXURL    = "MULTICA_DASHBOARD_FX_URL"
	envDashboardFXDefCNY = "MULTICA_DASHBOARD_FX_DEFAULT_CNY"
	envDashboardFXTTL    = "MULTICA_DASHBOARD_FX_TTL_SECONDS"
)

// DashboardRatesResponse is the wire shape of GET /api/dashboard/rates.
type DashboardRatesResponse struct {
	Rate        float64  `json:"rate"`
	Source      string   `json:"source"` // "live" | "default"
	UpdatedAt   *string  `json:"updated_at"`
	DefaultRate float64  `json:"default_rate"`
}

// DashboardRatesService fetches and caches the USD→CNY rate. Safe for
// concurrent use; one instance is built in handler.New.
type DashboardRatesService struct {
	mu    sync.Mutex
	cache *dashboardRatesCache
	http  *http.Client
}

type dashboardRatesCache struct {
	rate      float64
	source    string
	updatedAt time.Time
	fetchedAt time.Time
}

// fxAPIResponse is the minimal parse of the external FX payloads we accept:
// open.er-api.com returns {"result":"success","rates":{"CNY":…}}, and the
// exchangerate.host family returns {"success":true,"rates":{…}}. Only the
// CNY rate is read.
type fxAPIResponse struct {
	Result  string             `json:"result"`
	Success bool               `json:"success"`
	Rates   map[string]float64 `json:"rates"`
}

// NewDashboardRatesService builds the FX service with the default HTTP
// transport and a bounded request timeout.
func NewDashboardRatesService() *DashboardRatesService {
	return &DashboardRatesService{
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

// dashboardFXDefaultRate reads the configured fallback rate, defaulting to
// 7.2. An explicitly empty env value also yields 7.2, so the endpoint always
// has a default to degrade to (API contract §11.3: only "no default
// configured AND fetch failed" produces 500 — which cannot happen here
// because a default always exists).
func dashboardFXDefaultRate() float64 {
	raw := os.Getenv(envDashboardFXDefCNY)
	if raw == "" {
		return dashboardFXDefaultCNY
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
		return v
	}
	slog.Warn("dashboard/rates: invalid MULTICA_DASHBOARD_FX_DEFAULT_CNY, using default", "value", raw)
	return dashboardFXDefaultCNY
}

func dashboardFXTTL() time.Duration {
	raw := os.Getenv(envDashboardFXTTL)
	if raw == "" {
		return dashboardFXDefaultTTL
	}
	if sec, err := strconv.Atoi(raw); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return dashboardFXDefaultTTL
}

func dashboardFXURL() string {
	if raw := os.Getenv(envDashboardFXURL); raw != "" {
		return raw
	}
	return dashboardFXDefaultURL
}

// round4 snaps a rate to 4 decimal places, matching the contract's precision.
func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// Get returns the effective rate for the caller: a fresh live rate when the
// cache is cold, otherwise the cached value. When the external fetch fails it
// degrades to the configured default with source="default".
func (s *DashboardRatesService) Get(ctx context.Context) DashboardRatesResponse {
	def := dashboardFXDefaultRate()
	ttl := dashboardFXTTL()

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if s.cache != nil && now.Sub(s.cache.fetchedAt) < ttl {
		return s.responseFromCache(s.cache, def)
	}

	if rate, err := s.fetchLive(ctx); err == nil {
		s.cache = &dashboardRatesCache{
			rate:      rate,
			source:    "live",
			updatedAt: now,
			fetchedAt: now,
		}
		return s.responseFromCache(s.cache, def)
	}

	slog.Warn("dashboard/rates: live fetch failed, degrading to default", "url", dashboardFXURL())
	s.cache = &dashboardRatesCache{
		rate:      def,
		source:    "default",
		fetchedAt: now,
	}
	return s.responseFromCache(s.cache, def)
}

func (s *DashboardRatesService) responseFromCache(c *dashboardRatesCache, def float64) DashboardRatesResponse {
	resp := DashboardRatesResponse{
		Rate:        round4(c.rate),
		Source:      c.source,
		DefaultRate: round4(def),
	}
	if c.source == "live" {
		iso := c.updatedAt.UTC().Format(time.RFC3339)
		resp.UpdatedAt = &iso
	}
	return resp
}

// fetchLive calls the external API and returns the CNY rate.
func (s *DashboardRatesService) fetchLive(ctx context.Context) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dashboardFXURL(), nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fx endpoint returned %d", resp.StatusCode)
	}
	var payload fxAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode fx payload: %w", err)
	}
	if payload.Result != "success" && !payload.Success {
		return 0, fmt.Errorf("fx endpoint reported failure")
	}
	cny, ok := payload.Rates["CNY"]
	if !ok || cny <= 0 {
		return 0, fmt.Errorf("fx payload missing CNY rate")
	}
	return cny, nil
}

// GetDashboardRates serves GET /api/dashboard/rates. It does not read the
// user's data, but it lives under the workspace-member dashboard group, so
// it carries the same membership gate as the other dashboard endpoints.
func (h *Handler) GetDashboardRates(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	svc := h.DashboardRates
	if svc == nil {
		svc = NewDashboardRatesService()
	}
	writeJSON(w, http.StatusOK, svc.Get(r.Context()))
}
