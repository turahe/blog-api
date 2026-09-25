package config

import (
	"errors"
	"net/textproto"
	"strings"
)

func (c *Config) loadAnalytics() {
	c.AnalyticsIngestPerMinute = integer("ANALYTICS_INGEST_PER_MINUTE", 300)
	c.AnalyticsQueueSize = integer("ANALYTICS_QUEUE_SIZE", 10_000)
	c.AnalyticsCountryHeader = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(env("ANALYTICS_COUNTRY_HEADER", "")))
}

// ValidateAnalytics requires a positive ingest queue, a non-negative rate limit, and a
// country header that is only honoured behind trusted proxies.
func (c Config) ValidateAnalytics() error {
	if c.AnalyticsQueueSize < 1 {
		return errors.New("ANALYTICS_QUEUE_SIZE must be positive")
	}

	if c.AnalyticsIngestPerMinute < 0 {
		return errors.New("ANALYTICS_INGEST_PER_MINUTE must not be negative")
	}

	if c.AnalyticsCountryHeader != "" && len(c.TrustedProxies) == 0 {
		return errors.New("ANALYTICS_COUNTRY_HEADER requires APP_TRUSTED_PROXIES: the header is read only from trusted proxies")
	}

	return nil
}
