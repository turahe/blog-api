package responses

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildResponseCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		httpStatus  int
		serviceCode int
		caseCode    int
		want        int
	}{
		{
			name:        "created comments success",
			httpStatus:  201,
			serviceCode: ServiceComments,
			caseCode:    CaseSuccess,
			want:        2010301,
		},
		{
			name:        "ok posts success",
			httpStatus:  200,
			serviceCode: ServicePosts,
			caseCode:    CaseSuccess,
			want:        2000401,
		},
		{
			name:        "not found auth",
			httpStatus:  404,
			serviceCode: ServiceAuth,
			caseCode:    CaseNotFound,
			want:        4040105,
		},
		{
			name:        "validation platform",
			httpStatus:  400,
			serviceCode: ServicePlatform,
			caseCode:    CaseValidation,
			want:        4000002,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := BuildResponseCode(tt.httpStatus, tt.serviceCode, tt.caseCode)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCaseCodeForStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   int
	}{
		{name: "ok", status: 200, want: CaseSuccess},
		{name: "created", status: 201, want: CaseSuccess},
		{name: "accepted", status: 202, want: CaseAccepted},
		{name: "no content", status: 204, want: CaseSuccess},
		{name: "other 2xx", status: 206, want: CaseSuccess},
		{name: "bad request", status: 400, want: CaseValidation},
		{name: "unauthorized", status: 401, want: CaseUnauthorized},
		{name: "forbidden", status: 403, want: CaseForbidden},
		{name: "not found", status: 404, want: CaseNotFound},
		{name: "conflict", status: 409, want: CaseConflict},
		{name: "unprocessable", status: 422, want: CaseUnprocessable},
		{name: "rate limited", status: 429, want: CaseRateLimited},
		{name: "internal", status: 500, want: CaseInternalError},
		{name: "other 5xx", status: 503, want: CaseInternalError},
		{name: "unmapped 4xx", status: 418, want: CaseInternalError},
		{name: "redirect", status: 302, want: CaseInternalError},
		{name: "informational", status: 100, want: CaseInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, CaseCodeForStatus(tt.status))
		})
	}
}
