package requests

import (
	"testing"
)

func TestCreateCommentValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"content": "Nice post"}

	runBindCases[CreateComment](t, []bindCase{
		{name: "valid minimal", body: with(base)},
		{
			name: "valid guest reply",
			body: with(base, "parentId", idA.String(), "authorName", "Guest", "authorEmail", "g@example.com", "turnstileResponse", "tok"),
		},
		{name: "content required", body: with(base, "content", absent), want: errs("content", msgRequired("content"))},
		{name: "content too long", body: with(base, "content", long(10001)), want: errs("content", msgMaxChars("content", 10000))},
		{name: "parentId must be a uuid", body: with(base, "parentId", "x"), want: errs("parentId", msgUUID("parentId"))},
		{name: "authorName too long", body: with(base, "authorName", long(101)), want: errs("authorName", msgMaxChars("authorName", 100))},
		{name: "authorEmail format", body: with(base, "authorEmail", "nope"), want: errs("authorEmail", msgEmail("authorEmail"))},
		{name: "honeypot too long", body: with(base, "honeypot", long(501)), want: errs("honeypot", msgMaxChars("honeypot", 500))},
		{
			name: "turnstile too long",
			body: with(base, "turnstileResponse", long(2049)),
			want: errs("turnstileResponse", msgMaxChars("turnstileResponse", 2048)),
		},
	})
}

func TestUpdateCommentValidation(t *testing.T) {
	t.Parallel()

	runBindCases[UpdateComment](t, []bindCase{
		{name: "valid", body: `{"content":"edited"}`},
		{name: "content required", body: `{}`, want: errs("content", msgRequired("content"))},
		{name: "content too long", body: with(nil, "content", long(10001)), want: errs("content", msgMaxChars("content", 10000))},
	})
}

func TestModerateCommentValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"action": "approve"}

	runBindCases[ModerateComment](t, []bindCase{
		{name: "valid", body: with(base, "action", "spam", "reason", "ads", "notifyAuthor", true)},
		{name: "action required", body: with(base, "action", absent), want: errs("action", msgRequired("action"))},
		{name: "action unknown", body: with(base, "action", "delete"), want: errs("action", msgOneOf("action"))},
		{name: "reason too long", body: with(base, "reason", long(1001)), want: errs("reason", msgMaxChars("reason", 1000))},
	})
}

func TestBulkModerateCommentsValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"ids": []string{idA.String(), idB.String()}, "action": "reject"}

	runBindCases[BulkModerateComments](t, []bindCase{
		{name: "valid", body: with(base, "reason", "off topic")},
		{name: "ids required", body: with(base, "ids", absent), want: errs("ids", msgRequired("ids"))},
		{name: "ids empty", body: with(base, "ids", []string{}), want: errs("ids", msgMinItems("ids", 1))},
		{name: "id must be a uuid", body: with(base, "ids", []string{idA.String(), "x"}), want: errs("ids.1", msgUUID("ids.1"))},
		{name: "action unknown", body: with(base, "action", "delete"), want: errs("action", msgOneOf("action"))},
		{name: "reason too long", body: with(base, "reason", long(1001)), want: errs("reason", msgMaxChars("reason", 1000))},
	})
}

func TestFlagCommentValidation(t *testing.T) {
	t.Parallel()

	base := map[string]any{"reasonCode": "spam"}

	runBindCases[FlagComment](t, []bindCase{
		{name: "valid", body: with(base, "reasonCode", "self_harm", "details", "see link")},
		{name: "reason required", body: with(base, "reasonCode", absent), want: errs("reasonCode", msgRequired("reasonCode"))},
		{name: "reason unknown", body: with(base, "reasonCode", "boring"), want: errs("reasonCode", msgOneOf("reasonCode"))},
		{name: "details too long", body: with(base, "details", long(2001)), want: errs("details", msgMaxChars("details", 2000))},
	})
}
