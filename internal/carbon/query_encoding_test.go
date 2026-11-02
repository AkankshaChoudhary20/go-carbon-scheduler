package carbon

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type queryTransport func(*http.Request) (*http.Response, error)

func (f queryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestForecastEncodesZoneAndPreservesBaseQuery(t *testing.T) {
	client := &http.Client{Transport: queryTransport(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Query().Get("zone"); got != "A&B + C" {
			t.Errorf("zone = %q", got)
		}
		if got := r.URL.Query().Get("tenant"); got != "demo" {
			t.Errorf("tenant = %q", got)
		}
		if got := r.URL.Query().Get("horizonHours"); got != "6" {
			t.Errorf("horizon = %q", got)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"forecast":[]}`)), Header: make(http.Header)}, nil
	})}
	now := time.Now()
	p := ElectricityMapsProvider{APIToken: "test", BaseURL: "http://forecast.test?tenant=demo", HTTPClient: client}
	if _, err := p.Forecast(context.Background(), "A&B + C", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}
