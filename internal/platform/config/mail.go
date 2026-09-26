package config

import (
	"errors"
	"fmt"
	"strings"
)

// Mail drivers for account, notification, and newsletter confirmation email.
const (
	MailDriverSMTP   = "smtp"
	MailDriverResend = "resend"
)

func (c *Config) loadMail() {
	c.MailDriver = strings.ToLower(strings.TrimSpace(env("MAIL_DRIVER", MailDriverSMTP)))
	c.MailFrom = env("MAIL_FROM", env("SMTP_FROM", "Blog <blog@localhost>"))
	c.ResendAPIKey = env("RESEND_API_KEY", "")
}

// ValidateMail checks the driver selection and, for resend, its API key. The smtp driver
// with an empty SMTP_HOST is valid: emails then stay in the log.
func (c Config) ValidateMail() error {
	switch c.MailDriver {
	case MailDriverSMTP:
		return nil
	case MailDriverResend:
		if c.ResendAPIKey == "" {
			return errors.New("RESEND_API_KEY is required when MAIL_DRIVER=resend")
		}

		return nil
	default:
		return fmt.Errorf("MAIL_DRIVER must be smtp or resend (got %q)", c.MailDriver)
	}
}
