package config

import (
	"strings"
	"testing"
)

func TestLoadMail(t *testing.T) {
	cases := map[string]struct {
		env          map[string]string
		wantDriver   string
		wantFrom     string
		wantProvider string
	}{
		"defaults": {
			env:        map[string]string{},
			wantDriver: MailDriverSMTP, wantFrom: "Blog <blog@localhost>", wantProvider: NewsletterProviderSMTP,
		},
		"smtp from still read": {
			env:        map[string]string{"SMTP_FROM": "Old <old@example.test>"},
			wantDriver: MailDriverSMTP, wantFrom: "Old <old@example.test>", wantProvider: NewsletterProviderSMTP,
		},
		"resend drives the newsletter too": {
			env: map[string]string{
				"MAIL_DRIVER": "Resend", "RESEND_API_KEY": "re_test",
				"MAIL_FROM": "Blog <blog@example.test>", "SMTP_FROM": "Old <old@example.test>",
			},
			wantDriver: MailDriverResend, wantFrom: "Blog <blog@example.test>", wantProvider: NewsletterProviderResend,
		},
		"explicit newsletter provider wins": {
			env:        map[string]string{"MAIL_DRIVER": "resend", "RESEND_API_KEY": "re_test", "NEWSLETTER_PROVIDER": "smtp"},
			wantDriver: MailDriverResend, wantFrom: "Blog <blog@localhost>", wantProvider: NewsletterProviderSMTP,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setJWTKeys(t)

			for _, key := range []string{"MAIL_DRIVER", "MAIL_FROM", "SMTP_FROM", "RESEND_API_KEY", "NEWSLETTER_PROVIDER"} {
				t.Setenv(key, tc.env[key])
			}

			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}

			if cfg.MailDriver != tc.wantDriver || cfg.MailFrom != tc.wantFrom || cfg.NewsletterProvider != tc.wantProvider {
				t.Fatalf("driver=%q from=%q provider=%q", cfg.MailDriver, cfg.MailFrom, cfg.NewsletterProvider)
			}
		})
	}
}

func TestValidateMail(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		cfg  Config
		want string
	}{
		"smtp":                 {Config{MailDriver: "smtp"}, ""},
		"resend":               {Config{MailDriver: "resend", ResendAPIKey: "re_test"}, ""},
		"resend without key":   {Config{MailDriver: "resend"}, "RESEND_API_KEY"},
		"unknown driver":       {Config{MailDriver: "sendgrid"}, "MAIL_DRIVER"},
		"newsletter resend":    {Config{NewsletterProvider: "resend", NewsletterSendBatch: 50}, "RESEND_API_KEY"},
		"newsletter resend ok": {Config{NewsletterProvider: "resend", NewsletterSendBatch: 50, ResendAPIKey: "re_test"}, ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.cfg.ValidateMail()
			if tc.cfg.NewsletterProvider != "" {
				err = tc.cfg.ValidateNewsletter()
			}

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
