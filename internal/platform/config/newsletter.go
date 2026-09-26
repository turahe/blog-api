package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Newsletter providers.
const (
	NewsletterProviderSMTP       = "smtp"
	NewsletterProviderResend     = "resend"
	NewsletterProviderCustomHTTP = "custom_http"

	// minNewsletterSecret is the shortest accepted HMAC secret, in bytes.
	minNewsletterSecret = 32
	maxNewsletterBatch  = 500
)

// loadNewsletter must run after loadMail: an unset NEWSLETTER_PROVIDER follows MAIL_DRIVER.
func (c *Config) loadNewsletter() {
	fallback := NewsletterProviderSMTP
	if c.MailDriver == MailDriverResend {
		fallback = NewsletterProviderResend
	}

	c.NewsletterProvider = strings.ToLower(strings.TrimSpace(env("NEWSLETTER_PROVIDER", fallback)))
	c.NewsletterHTTPEndpoint = strings.TrimSpace(env("NEWSLETTER_HTTP_ENDPOINT", ""))
	c.NewsletterHTTPSecret = strings.TrimSpace(env("NEWSLETTER_HTTP_SECRET", ""))
	c.NewsletterSendBatch = integer("NEWSLETTER_SEND_BATCH", 50)
}

// NewsletterWebhookEnabled reports whether the signed provider webhook is accepted.
func (c Config) NewsletterWebhookEnabled() bool { return c.NewsletterHTTPSecret != "" }

// ValidateNewsletter checks the provider selection and its credentials. A secret set with the
// smtp or resend provider enables only the bounce webhook.
func (c Config) ValidateNewsletter() error {
	if c.NewsletterSendBatch < 1 || c.NewsletterSendBatch > maxNewsletterBatch {
		return fmt.Errorf("NEWSLETTER_SEND_BATCH must be between 1 and %d", maxNewsletterBatch)
	}

	if c.NewsletterHTTPSecret != "" && len(c.NewsletterHTTPSecret) < minNewsletterSecret {
		return fmt.Errorf("NEWSLETTER_HTTP_SECRET must be at least %d bytes", minNewsletterSecret)
	}

	switch c.NewsletterProvider {
	case NewsletterProviderSMTP:
		return nil
	case NewsletterProviderResend:
		if c.ResendAPIKey == "" {
			return errors.New("RESEND_API_KEY is required when NEWSLETTER_PROVIDER=resend")
		}

		return nil
	case NewsletterProviderCustomHTTP:
		return c.validateNewsletterHTTP()
	default:
		return fmt.Errorf("NEWSLETTER_PROVIDER must be smtp, resend, or custom_http (got %q)", c.NewsletterProvider)
	}
}

func (c Config) validateNewsletterHTTP() error {
	if c.NewsletterHTTPSecret == "" {
		return errors.New("NEWSLETTER_HTTP_SECRET is required when NEWSLETTER_PROVIDER=custom_http")
	}

	endpoint, err := url.Parse(c.NewsletterHTTPEndpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return errors.New("NEWSLETTER_HTTP_ENDPOINT must be an http(s) URL when NEWSLETTER_PROVIDER=custom_http")
	}

	if endpoint.Scheme == "http" && c.Environment == envProduction {
		return errors.New("NEWSLETTER_HTTP_ENDPOINT must use https in production")
	}

	return nil
}
