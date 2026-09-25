package config

import (
	"strings"
	"testing"
)

func TestLoadNewsletterDefaults(t *testing.T) {
	setJWTKeys(t)
	t.Setenv("NEWSLETTER_PROVIDER", "")
	t.Setenv("NEWSLETTER_HTTP_ENDPOINT", "")
	t.Setenv("NEWSLETTER_HTTP_SECRET", "")
	t.Setenv("NEWSLETTER_SEND_BATCH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.NewsletterProvider != NewsletterProviderSMTP || cfg.NewsletterSendBatch != 50 || cfg.NewsletterWebhookEnabled() {
		t.Fatalf("provider=%q batch=%d webhook=%v", cfg.NewsletterProvider, cfg.NewsletterSendBatch, cfg.NewsletterWebhookEnabled())
	}
}

func TestValidateNewsletter(t *testing.T) {
	t.Parallel()

	secret := strings.Repeat("s", 32)

	cases := map[string]struct {
		cfg  Config
		want string
	}{
		"smtp":                  {Config{NewsletterProvider: "smtp", NewsletterSendBatch: 50}, ""},
		"smtp with webhook":     {Config{NewsletterProvider: "smtp", NewsletterSendBatch: 50, NewsletterHTTPSecret: secret}, ""},
		"unknown provider":      {Config{NewsletterProvider: "mailchimp", NewsletterSendBatch: 50}, "NEWSLETTER_PROVIDER"},
		"batch too large":       {Config{NewsletterProvider: "smtp", NewsletterSendBatch: 501}, "NEWSLETTER_SEND_BATCH"},
		"short secret":          {Config{NewsletterProvider: "smtp", NewsletterSendBatch: 50, NewsletterHTTPSecret: "short"}, "at least 32"},
		"custom without secret": {Config{NewsletterProvider: "custom_http", NewsletterSendBatch: 50, NewsletterHTTPEndpoint: "https://gw.test/send"}, "NEWSLETTER_HTTP_SECRET"},
		"custom without endpoint": {
			Config{NewsletterProvider: "custom_http", NewsletterSendBatch: 50, NewsletterHTTPSecret: secret}, "NEWSLETTER_HTTP_ENDPOINT",
		},
		"custom http in production": {Config{
			Environment: "production", NewsletterProvider: "custom_http", NewsletterSendBatch: 50,
			NewsletterHTTPSecret: secret, NewsletterHTTPEndpoint: "http://gw.test/send",
		}, "https"},
		"custom valid": {Config{
			Environment: "production", NewsletterProvider: "custom_http", NewsletterSendBatch: 50,
			NewsletterHTTPSecret: secret, NewsletterHTTPEndpoint: "https://gw.test/send",
		}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.cfg.ValidateNewsletter()
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
