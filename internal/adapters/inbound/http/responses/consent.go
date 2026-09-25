package responses

import (
	"github.com/gin-gonic/gin"
	consentdomain "github.com/turahe/blog-api/internal/core/consent/domain"
	consentservice "github.com/turahe/blog-api/internal/core/consent/service"
)

// ConsentState serializes a consent subject's decisions. token appears only when the
// subject was just created.
func ConsentState(state consentservice.State) gin.H {
	consents := make([]gin.H, 0, len(state.Consents))
	for _, c := range state.Consents {
		consents = append(consents, Consent(c))
	}

	out := gin.H{
		"subject_id":                     state.Subject.UUID,
		"authenticated_analytics_linked": state.Subject.UserUUID != nil,
		"consents":                       consents,
	}
	if state.Token != "" {
		out["token"] = state.Token
	}

	return out
}

// Consent serializes one purpose's decision.
func Consent(c consentdomain.Consent) gin.H {
	return gin.H{
		"id":             c.UUID,
		"purpose":        string(c.Purpose),
		"status":         string(c.Status),
		"policy_version": c.PolicyVersion,
		"decided_at":     c.DecidedAt,
		"withdrawn_at":   c.WithdrawnAt,
	}
}
