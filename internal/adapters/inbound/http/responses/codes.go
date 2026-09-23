package responses

// Service codes (2 digits) identify the domain that produced the response.
// Keep this table in sync with docs/backend/response-codes.md.
const (
	ServicePlatform      = 0  // health, generic/platform
	ServiceAuth          = 1  // auth, sessions, password reset
	ServiceUsers         = 2  // users, profiles, me/*
	ServiceComments      = 3  // comments & moderation
	ServicePosts         = 4  // posts, revisions, SEO
	ServiceMedia         = 5  // media uploads & assets
	ServiceCategories    = 6  // categories
	ServiceTags          = 7  // tags
	ServiceNotifications = 8  // notifications & SSE
	ServiceAnalytics     = 9  // analytics ingest & reports
	ServiceSettings      = 10 // admin settings
	ServiceRBAC          = 11 // roles, permissions, impersonation
	ServiceNewsletter    = 12 // newsletter
)

// Case codes (2 digits) identify the outcome within a service.
const (
	CaseSuccess       = 1
	CaseValidation    = 2
	CaseUnauthorized  = 3
	CaseForbidden     = 4
	CaseNotFound      = 5
	CaseConflict      = 6
	CaseUnprocessable = 7
	CaseRateLimited   = 8
	CaseInternalError = 9
	CaseAccepted      = 10 // async / queued acceptance
	CaseNoContent     = 11 // empty success / deleted
)

// BuildResponseCode builds a response code from HTTP status, service code, and case code.
// Format: HTTP_STATUS_CODE (3 digits) + SERVICE_CODE (2 digits) + CASE_CODE (2 digits)
// Example: 2010301 = HTTP 201 + Service 03 (Comments) + Case 01 (Success)
func BuildResponseCode(httpStatus, serviceCode, caseCode int) int {
	return (httpStatus%1000)*10000 + (serviceCode%100)*100 + (caseCode % 100)
}

// CaseCodeForStatus maps common HTTP statuses to a default case code.
func CaseCodeForStatus(status int) int {
	switch status {
	case 200, 201, 204:
		return CaseSuccess
	case 202:
		return CaseAccepted
	case 400:
		return CaseValidation
	case 401:
		return CaseUnauthorized
	case 403:
		return CaseForbidden
	case 404:
		return CaseNotFound
	case 409:
		return CaseConflict
	case 422:
		return CaseUnprocessable
	case 429:
		return CaseRateLimited
	default:
		if status >= 500 {
			return CaseInternalError
		}
		if status >= 200 && status < 300 {
			return CaseSuccess
		}
		return CaseInternalError
	}
}
