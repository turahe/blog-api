package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	auditdomain "github.com/turahe/blog-api/internal/core/audit/domain"
)

var (
	auditEntryID = uuid.MustParse("0198a1b2-0000-7000-8000-00000000a001")
	auditActorID = uuid.MustParse("0198a1b2-0000-7000-8000-00000000a002")
	auditStaffID = uuid.MustParse("0198a1b2-0000-7000-8000-00000000a003")
	auditPostID  = uuid.MustParse("0198a1b2-0000-7000-8000-00000000a004")
	auditAt      = time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
)

const firefoxLinuxUA = "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0"

func fullAuditEntry() auditdomain.Entry {
	return auditdomain.Entry{
		UUID:           auditEntryID,
		Action:         "posts.update",
		Category:       "content",
		ActorID:        &auditActorID,
		ImpersonatorID: &auditStaffID,
		ResourceType:   "post",
		ResourceID:     &auditPostID,
		Result:         "success",
		Status:         200,
		Changes:        map[string]auditdomain.Change{"title": {From: "Old", To: "New"}},
		Metadata:       map[string]any{"source": "editor"},
		IP:             "203.0.113.77",
		UserAgent:      firefoxLinuxUA,
		RequestID:      "req-1",
		OccurredAt:     auditAt,
	}
}

func TestOwnerActivity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry auditdomain.Entry
		want  string
	}{
		{
			name:  "impersonated action with client details",
			entry: fullAuditEntry(),
			want: `{"id": "0198a1b2-0000-7000-8000-00000000a001", "category": "content", "action": "posts.update",
				"result": "success", "ipPrefix": "203.0.113.0/24", "device": "Firefox on Linux",
				"impersonated": true, "occurredAt": "2026-08-01T10:00:00Z"}`,
		},
		{
			name:  "own action without client details",
			entry: auditdomain.Entry{UUID: auditEntryID, Action: "auth.login", Category: "security", Result: "failure", OccurredAt: auditAt},
			want: `{"id": "0198a1b2-0000-7000-8000-00000000a001", "category": "security", "action": "auth.login",
				"result": "failure", "ipPrefix": null, "device": null,
				"impersonated": false, "occurredAt": "2026-08-01T10:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, OwnerActivity(tt.entry))
		})
	}
}

func TestAdminActivity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry auditdomain.Entry
		want  string
	}{
		{
			name:  "full entry",
			entry: fullAuditEntry(),
			want: `{"id": "0198a1b2-0000-7000-8000-00000000a001", "action": "posts.update", "category": "content",
				"result": "success", "status": 200,
				"actorId": "0198a1b2-0000-7000-8000-00000000a002", "impersonatorId": "0198a1b2-0000-7000-8000-00000000a003",
				"resourceType": "post", "resourceId": "0198a1b2-0000-7000-8000-00000000a004",
				"changes": {"title": {"from": "Old", "to": "New"}}, "metadata": {"source": "editor"},
				"ip": "203.0.113.77", "userAgent": "` + firefoxLinuxUA + `", "requestId": "req-1",
				"occurredAt": "2026-08-01T10:00:00Z"}`,
		},
		{
			name:  "empty strings become null",
			entry: auditdomain.Entry{UUID: auditEntryID, Action: "system.prune", Result: "success", Status: 204, OccurredAt: auditAt},
			want: `{"id": "0198a1b2-0000-7000-8000-00000000a001", "action": "system.prune", "category": null,
				"result": "success", "status": 204, "actorId": null, "impersonatorId": null,
				"resourceType": null, "resourceId": null, "changes": null, "metadata": null,
				"ip": null, "userAgent": null, "requestId": null, "occurredAt": "2026-08-01T10:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AdminActivity(tt.entry))
		})
	}
}

func TestNullable(t *testing.T) {
	t.Parallel()

	assert.Nil(t, nullable(""))
	assert.Equal(t, new("x"), nullable("x"))
}

func TestIPPrefixCoarsensAddresses(t *testing.T) {
	t.Parallel()

	cases := map[string]*string{
		"203.0.113.77":        new("203.0.113.0/24"),
		"::ffff:198.51.100.9": new("198.51.100.0/24"),
		"2001:db8:abcd:12::1": new("2001:db8:abcd::/48"),
		"":                    nil,
		"not-an-ip":           nil,
	}

	for in, want := range cases {
		require.Equal(t, want, ipPrefix(in), in)
	}
}

func TestDeviceSummarizesUserAgent(t *testing.T) {
	t.Parallel()

	cases := map[string]*string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/128.0 Safari/537.36 Edg/128.0":     new("Edge on Windows"),
		"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/128.0 Mobile Safari/537.36":                  new("Chrome on Android"),
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Safari/604.1": new("Safari on iOS"),
		"curl/8.5.0":  new("curl"),
		"SomeBot/1.0": new("Unknown browser"),
		"":            nil,
	}

	for in, want := range cases {
		require.Equal(t, want, device(in), in)
	}
}
