package requests

// StoreConsent is POST /api/v1/analytics/consent: a decision per purpose and the version
// of the consent policy the visitor was shown.
type StoreConsent struct {
	Purposes      map[string]bool `json:"purposes"       binding:"required,min=1,max=10"`
	PolicyVersion string          `json:"policyVersion" binding:"required,max=32"`
}
