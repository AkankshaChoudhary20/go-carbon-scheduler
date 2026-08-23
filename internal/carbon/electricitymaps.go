package carbon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const electricityMapsBaseURL = "https://api.electricitymap.org/v4/carbon-intensity/forecast"

// ElectricityMapsProvider fetches real grid carbon-intensity forecasts from
// the ElectricityMaps API (https://app.electricitymaps.com/api). Requires a
// free-tier API token — see the README for how to get one.
type ElectricityMapsProvider struct {
	APIToken   string
	HTTPClient *http.Client
	BaseURL    string // overridable for tests; defaults to electricityMapsBaseURL
}

type electricityMapsResponse struct {
	Zone     string `json:"zone"`
	Forecast []struct {
		CarbonIntensity float64 `json:"carbonIntensity"`
		Datetime        string  `json:"datetime"`
	} `json:"forecast"`
}

func (e ElectricityMapsProvider) Forecast(ctx context.Context, zone string, from, to time.Time) ([]Point, error) {
	if e.APIToken == "" {
		return nil, fmt.Errorf("electricitymaps: no API token configured")
	}

	base := e.BaseURL
	if base == "" {
		base = electricityMapsBaseURL
	}
	client := e.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	horizonHours := int(to.Sub(from).Hours()) + 1
	horizon := nearestSupportedHorizon(horizonHours)

	url := fmt.Sprintf("%s?zone=%s&horizonHours=%s", base, zone, strconv.Itoa(horizon))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("electricitymaps: build request: %w", err)
	}
	req.Header.Set("auth-token", e.APIToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("electricitymaps: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("electricitymaps: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("electricitymaps: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var parsed electricityMapsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("electricitymaps: parse response: %w", err)
	}

	points := make([]Point, 0, len(parsed.Forecast))
	for _, f := range parsed.Forecast {
		t, err := time.Parse(time.RFC3339, f.Datetime)
		if err != nil {
			continue // skip malformed points rather than failing the whole forecast
		}
		if t.Before(from) || t.After(to) {
			continue
		}
		points = append(points, Point{Time: t, GramsCO2PerKWh: f.CarbonIntensity})
	}
	return points, nil
}

// nearestSupportedHorizon rounds up to one of ElectricityMaps' supported
// horizonHours values (6, 24, 48, 72), capping at the maximum.
func nearestSupportedHorizon(hours int) int {
	for _, h := range []int{6, 24, 48, 72} {
		if hours <= h {
			return h
		}
	}
	return 72
}
