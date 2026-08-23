package carbon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestElectricityMapsProviderParsesFixture(t *testing.T) {
	const fixture = `{
		"zone": "US-CAL-CISO",
		"forecast": [
			{"carbonIntensity": 210.5, "datetime": "2026-08-24T00:00:00.000Z"},
			{"carbonIntensity": 190.2, "datetime": "2026-08-24T01:00:00.000Z"},
			{"carbonIntensity": 410.9, "datetime": "2026-08-24T18:00:00.000Z"}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("auth-token"); got != "test-token" {
			t.Errorf("missing/wrong auth-token header: %q", got)
		}
		if got := r.URL.Query().Get("zone"); got != "US-CAL-CISO" {
			t.Errorf("missing/wrong zone query param: %q", got)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(fixture))
	}))
	defer server.Close()

	provider := ElectricityMapsProvider{APIToken: "test-token", BaseURL: server.URL}
	from := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	points, err := provider.Forecast(context.Background(), "US-CAL-CISO", from, to)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("expected 3 points, got %d", len(points))
	}
	if points[0].GramsCO2PerKWh != 210.5 {
		t.Errorf("point 0 intensity = %v, want 210.5", points[0].GramsCO2PerKWh)
	}
}

func TestElectricityMapsProviderRequiresToken(t *testing.T) {
	provider := ElectricityMapsProvider{}
	_, err := provider.Forecast(context.Background(), "US-CAL-CISO", time.Now(), time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected error when no API token is configured")
	}
}

func TestElectricityMapsProviderHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid token"}`))
	}))
	defer server.Close()

	provider := ElectricityMapsProvider{APIToken: "bad-token", BaseURL: server.URL}
	_, err := provider.Forecast(context.Background(), "US-CAL-CISO", time.Now(), time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected error on HTTP 401")
	}
}
