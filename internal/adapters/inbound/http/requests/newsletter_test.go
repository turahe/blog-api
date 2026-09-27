package requests

import (
	"testing"
)

func TestNewsletterSubscribeValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"email": "reader@example.com"}

	runBindCases[NewsletterSubscribe](t, []bindCase{
		{name: "valid", body: with(base, "displayName", "Alex", "lists", []string{"weekly"}, "format", "plaintext")},
		{name: "email required", body: with(base, "email", absent), want: errs("email", msgRequired("email"))},
		{name: "email too long", body: with(base, "email", long(255)), want: errs("email", msgMaxChars("email", 254))},
		{name: "displayName too long", body: with(base, "displayName", long(101)), want: errs("displayName", msgMaxChars("displayName", 100))},
		{name: "too many lists", body: with(base, "lists", make([]string, 21)), want: errs("lists", msgMaxItems("lists", 20))},
		{name: "empty list", body: with(base, "lists", []string{""}), want: errs("lists.0", msgRequired("lists.0"))},
		{name: "list too long", body: with(base, "lists", []string{long(65)}), want: errs("lists.0", msgMaxChars("lists.0", 64))},
		{name: "format unknown", body: with(base, "format", "pdf"), want: errs("format", msgOneOf("format"))},
		{name: "honeypot too long", body: with(base, "honeypot", long(201)), want: errs("honeypot", msgMaxChars("honeypot", 200))},
		{
			name: "turnstile too long",
			body: with(base, "turnstileResponse", long(2049)),
			want: errs("turnstileResponse", msgMaxChars("turnstileResponse", 2048)),
		},
	})
}

func TestNewsletterTokenValidation(t *testing.T) {
	t.Parallel()

	runBindCases[NewsletterToken](t, []bindCase{
		{name: "valid", body: `{"token":"t"}`},
		{name: "token required", body: `{}`, want: errs("token", msgRequired("token"))},
		{name: "token too long", body: with(nil, "token", long(129)), want: errs("token", msgMaxChars("token", 128))},
	})
}

func TestNewsletterResendValidation(t *testing.T) {
	t.Parallel()

	runBindCases[NewsletterResend](t, []bindCase{
		{name: "valid", body: `{"email":"reader@example.com"}`},
		{name: "email required", body: `{}`, want: errs("email", msgRequired("email"))},
		{name: "email too long", body: with(nil, "email", long(255)), want: errs("email", msgMaxChars("email", 254))},
	})
}

func TestNewsletterUnsubscribeValidation(t *testing.T) {
	t.Parallel()

	runBindCases[NewsletterUnsubscribe](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "valid", body: with(nil, "token", "t", "reasonCode", "too_frequent", "feedback", "less please")},
		{name: "token too long", body: with(nil, "token", long(129)), want: errs("token", msgMaxChars("token", 128))},
		{name: "reason unknown", body: with(nil, "reasonCode", "bored"), want: errs("reasonCode", msgOneOf("reasonCode"))},
		{name: "feedback too long", body: with(nil, "feedback", long(1001)), want: errs("feedback", msgMaxChars("feedback", 1000))},
	})
}

func TestNewsletterPreferencesValidation(t *testing.T) {
	t.Parallel()

	runBindCases[NewsletterPreferences](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "valid", body: with(nil, "format", "html", "lists", []string{"weekly"}, "unsubscribeAll", false)},
		{name: "format unknown", body: with(nil, "format", "pdf"), want: errs("format", msgOneOf("format"))},
		{name: "too many lists", body: with(nil, "lists", make([]string, 21)), want: errs("lists", msgMaxItems("lists", 20))},
		{name: "empty list", body: with(nil, "lists", []string{""}), want: errs("lists.0", msgRequired("lists.0"))},
		{name: "list too long", body: with(nil, "lists", []string{long(65)}), want: errs("lists.0", msgMaxChars("lists.0", 64))},
	})
}

func TestNewsletterMeSubscribeValidation(t *testing.T) {
	t.Parallel()

	runBindCases[NewsletterMeSubscribe](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "valid", body: with(nil, "lists", []string{"weekly"}, "format", "plaintext")},
		{name: "too many lists", body: with(nil, "lists", make([]string, 21)), want: errs("lists", msgMaxItems("lists", 20))},
		{name: "empty list", body: with(nil, "lists", []string{""}), want: errs("lists.0", msgRequired("lists.0"))},
		{name: "format unknown", body: with(nil, "format", "pdf"), want: errs("format", msgOneOf("format"))},
	})
}

func TestNewsletterMeUnsubscribeValidation(t *testing.T) {
	t.Parallel()

	runBindCases[NewsletterMeUnsubscribe](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "valid", body: with(nil, "lists", []string{"weekly"}, "reasonCode", "other", "feedback", "bye")},
		{name: "list too long", body: with(nil, "lists", []string{long(65)}), want: errs("lists.0", msgMaxChars("lists.0", 64))},
		{name: "reason unknown", body: with(nil, "reasonCode", "bored"), want: errs("reasonCode", msgOneOf("reasonCode"))},
		{name: "feedback too long", body: with(nil, "feedback", long(1001)), want: errs("feedback", msgMaxChars("feedback", 1000))},
	})
}

func TestNewsletterIssueCreateValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"subject": "Hi", "bodyMarkdown": "## Hello", "lists": []string{"weekly"}}

	runBindCases[NewsletterIssueCreate](t, []bindCase{
		{name: "valid", body: with(base, "preheader", "p", "status", "scheduled", "sendAt", "2026-10-01T09:00:00Z")},
		{name: "subject required", body: with(base, "subject", absent), want: errs("subject", msgRequired("subject"))},
		{name: "subject too long", body: with(base, "subject", long(201)), want: errs("subject", msgMaxChars("subject", 200))},
		{name: "preheader too long", body: with(base, "preheader", long(201)), want: errs("preheader", msgMaxChars("preheader", 200))},
		{name: "body required", body: with(base, "bodyMarkdown", absent), want: errs("bodyMarkdown", msgRequired("bodyMarkdown"))},
		{name: "body too long", body: with(base, "bodyMarkdown", long(200001)), want: errs("bodyMarkdown", msgMaxChars("bodyMarkdown", 200000))},
		{name: "lists required", body: with(base, "lists", absent), want: errs("lists", msgRequired("lists"))},
		{name: "lists empty", body: with(base, "lists", []string{}), want: errs("lists", msgMinItems("lists", 1))},
		{name: "too many lists", body: with(base, "lists", make([]string, 21)), want: errs("lists", msgMaxItems("lists", 20))},
		{name: "empty list", body: with(base, "lists", []string{""}), want: errs("lists.0", msgRequired("lists.0"))},
		{name: "status unknown", body: with(base, "status", "cancelled"), want: errs("status", msgOneOf("status"))},
		{name: "sendAt must be a time", body: with(base, "sendAt", "tomorrow"), want: errs("_form", "The request body is invalid.")},
	})
}

func TestNewsletterIssuePatchValidation(t *testing.T) {
	t.Parallel()

	runBindCases[NewsletterIssuePatch](t, []bindCase{
		{name: "empty body is valid", body: `{}`},
		{name: "valid", body: with(nil, "subject", "s", "preheader", "p", "bodyMarkdown", "b", "lists", []string{"weekly"}, "status", "cancelled")},
		{name: "subject too long", body: with(nil, "subject", long(201)), want: errs("subject", msgMaxChars("subject", 200))},
		{name: "preheader too long", body: with(nil, "preheader", long(201)), want: errs("preheader", msgMaxChars("preheader", 200))},
		{name: "body too long", body: with(nil, "bodyMarkdown", long(200001)), want: errs("bodyMarkdown", msgMaxChars("bodyMarkdown", 200000))},
		{name: "lists empty", body: with(nil, "lists", []string{}), want: errs("lists", msgMinItems("lists", 1))},
		{name: "empty list", body: with(nil, "lists", []string{""}), want: errs("lists.0", msgRequired("lists.0"))},
		{name: "status unknown", body: with(nil, "status", "sent"), want: errs("status", msgOneOf("status"))},
	})
}

func TestNewsletterProviderConfigValidation(t *testing.T) {
	t.Parallel()

	list := map[string]any{"slug": "weekly", "name": "Weekly"}
	base := map[string]any{"confirmTtlHours": 48, "doubleOptinRequired": false, "lists": []any{list}}

	withList := func(pairs ...any) []any {
		fields := make(map[string]any, len(list))
		for k, v := range list {
			fields[k] = v
		}

		for i := 0; i+1 < len(pairs); i += 2 {
			if pairs[i+1] == absent {
				delete(fields, pairs[i].(string))
				continue
			}

			fields[pairs[i].(string)] = pairs[i+1]
		}

		return []any{fields}
	}

	runBindCases[NewsletterProviderConfig](t, []bindCase{
		{
			name: "valid",
			body: with(base, "fromName", "Blog", "fromEmail", "n@example.com", "replyTo", "r@example.com", "postalAddress", "Street 1"),
		},
		{name: "fromName too long", body: with(base, "fromName", long(101)), want: errs("fromName", msgMaxChars("fromName", 100))},
		{name: "fromEmail too long", body: with(base, "fromEmail", long(255)), want: errs("fromEmail", msgMaxChars("fromEmail", 254))},
		{name: "replyTo too long", body: with(base, "replyTo", long(255)), want: errs("replyTo", msgMaxChars("replyTo", 254))},
		{name: "postalAddress too long", body: with(base, "postalAddress", long(501)), want: errs("postalAddress", msgMaxChars("postalAddress", 500))},
		{name: "ttl required", body: with(base, "confirmTtlHours", absent), want: errs("confirmTtlHours", msgRequired("confirmTtlHours"))},
		{name: "ttl too small", body: with(base, "confirmTtlHours", -1), want: errs("confirmTtlHours", msgMin("confirmTtlHours", 1))},
		{name: "ttl too large", body: with(base, "confirmTtlHours", 169), want: errs("confirmTtlHours", msgMax("confirmTtlHours", 168))},
		{
			name: "double opt-in required",
			body: with(base, "doubleOptinRequired", absent),
			want: errs("doubleOptinRequired", msgRequired("doubleOptinRequired")),
		},
		{name: "lists required", body: with(base, "lists", absent), want: errs("lists", msgRequired("lists"))},
		{name: "lists empty", body: with(base, "lists", []any{}), want: errs("lists", msgMinItems("lists", 1))},
		{name: "list slug required", body: with(base, "lists", withList("slug", absent)), want: errs("lists.0.slug", msgRequired("lists.0.slug"))},
		{name: "list slug too long", body: with(base, "lists", withList("slug", long(65))), want: errs("lists.0.slug", msgMaxChars("lists.0.slug", 64))},
		{name: "list name required", body: with(base, "lists", withList("name", absent)), want: errs("lists.0.name", msgRequired("lists.0.name"))},
		{name: "list name too long", body: with(base, "lists", withList("name", long(101))), want: errs("lists.0.name", msgMaxChars("lists.0.name", 100))},
		{
			name: "list description too long",
			body: with(base, "lists", withList("description", long(501))),
			want: errs("lists.0.description", msgMaxChars("lists.0.description", 500)),
		},
	})
}
