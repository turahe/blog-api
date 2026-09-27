package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	consentdomain "github.com/turahe/blog-api/internal/core/consent/domain"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
)

var (
	consentSubjectID = uuid.MustParse("0198a1b2-0000-7000-8000-000000000c51")
	consentUserID    = uuid.MustParse("0198a1b2-0000-7000-8000-000000000c52")
	consentID        = uuid.MustParse("0198a1b2-0000-7000-8000-000000000c53")
	consentDecidedAt = time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
)

func grantedAnalytics() consentdomain.Consent {
	return consentdomain.Consent{
		UUID:          consentID,
		SubjectUUID:   consentSubjectID,
		Purpose:       consentdomain.PurposeAnalytics,
		Status:        consentdomain.StatusGranted,
		PolicyVersion: "2026-07",
		DecidedAt:     consentDecidedAt,
	}
}

const grantedAnalyticsJSON = `{"id": "0198a1b2-0000-7000-8000-000000000c53", "purpose": "analytics", "status": "granted",
	"policyVersion": "2026-07", "decidedAt": "2026-08-01T10:00:00Z", "withdrawnAt": null}`

func TestConsentState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state consentservice.State
		want  string
	}{
		{
			name:  "new anonymous subject gets its token",
			state: consentservice.State{Subject: consentdomain.Subject{UUID: consentSubjectID}, Token: "subject-token"},
			want: `{"subjectId": "0198a1b2-0000-7000-8000-000000000c51", "authenticatedAnalyticsLinked": false,
				"consents": [], "token": "subject-token"}`,
		},
		{
			name: "existing linked subject has no token",
			state: consentservice.State{
				Subject:  consentdomain.Subject{UUID: consentSubjectID, UserUUID: &consentUserID},
				Consents: []consentdomain.Consent{grantedAnalytics()},
			},
			want: `{"subjectId": "0198a1b2-0000-7000-8000-000000000c51", "authenticatedAnalyticsLinked": true,
				"consents": [` + grantedAnalyticsJSON + `]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, ConsentState(tt.state))
		})
	}
}

func TestConsent(t *testing.T) {
	t.Parallel()

	withdrawn := grantedAnalytics()
	withdrawn.Purpose = consentdomain.PurposeAuthenticatedAnalytics
	withdrawn.Status = consentdomain.StatusWithdrawn
	withdrawn.WithdrawnAt = new(consentDecidedAt.Add(time.Hour))

	tests := []struct {
		name    string
		consent consentdomain.Consent
		want    string
	}{
		{name: "granted", consent: grantedAnalytics(), want: grantedAnalyticsJSON},
		{
			name:    "withdrawn",
			consent: withdrawn,
			want: `{"id": "0198a1b2-0000-7000-8000-000000000c53", "purpose": "authenticated_analytics",
				"status": "withdrawn", "policyVersion": "2026-07", "decidedAt": "2026-08-01T10:00:00Z",
				"withdrawnAt": "2026-08-01T11:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, Consent(tt.consent))
		})
	}
}
