package config

import (
	"strings"
	"testing"
)

func TestLoadAnalyticsDefaults(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("ANALYTICS_INGEST_PER_MINUTE", "")
	t.Setenv("ANALYTICS_QUEUE_SIZE", "")
	t.Setenv("ANALYTICS_COUNTRY_HEADER", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.AnalyticsIngestPerMinute != 300 || cfg.AnalyticsQueueSize != 10_000 || cfg.AnalyticsCountryHeader != "" {
		t.Fatalf("per_minute=%d queue=%d header=%q", cfg.AnalyticsIngestPerMinute, cfg.AnalyticsQueueSize, cfg.AnalyticsCountryHeader)
	}
}

func TestLoadAnalyticsCanonicalisesTheCountryHeader(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("ANALYTICS_COUNTRY_HEADER", "cf-ipcountry")
	t.Setenv("APP_TRUSTED_PROXIES", "10.0.0.0/8")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.AnalyticsCountryHeader != "Cf-Ipcountry" {
		t.Fatalf("header=%q", cfg.AnalyticsCountryHeader)
	}
}

func TestValidateAnalytics(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		cfg  Config
		want string
	}{
		"defaults":         {Config{AnalyticsQueueSize: 10, AnalyticsIngestPerMinute: 300}, ""},
		"limit disabled":   {Config{AnalyticsQueueSize: 10}, ""},
		"no queue":         {Config{AnalyticsIngestPerMinute: 300}, "ANALYTICS_QUEUE_SIZE"},
		"negative limit":   {Config{AnalyticsQueueSize: 10, AnalyticsIngestPerMinute: -1}, "ANALYTICS_INGEST_PER_MINUTE"},
		"header no proxy":  {Config{AnalyticsQueueSize: 10, AnalyticsCountryHeader: "Cf-Ipcountry"}, "APP_TRUSTED_PROXIES"},
		"header and proxy": {Config{AnalyticsQueueSize: 10, AnalyticsCountryHeader: "Cf-Ipcountry", TrustedProxies: []string{"10.0.0.1"}}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.cfg.ValidateAnalytics()
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want %q", err, tc.want)
			}
		})
	}
}
