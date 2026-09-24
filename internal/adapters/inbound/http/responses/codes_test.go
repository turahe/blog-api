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
	require.Equal(t, CaseSuccess, CaseCodeForStatus(200))
	require.Equal(t, CaseSuccess, CaseCodeForStatus(201))
	require.Equal(t, CaseAccepted, CaseCodeForStatus(202))
	require.Equal(t, CaseValidation, CaseCodeForStatus(400))
	require.Equal(t, CaseUnauthorized, CaseCodeForStatus(401))
	require.Equal(t, CaseForbidden, CaseCodeForStatus(403))
	require.Equal(t, CaseNotFound, CaseCodeForStatus(404))
	require.Equal(t, CaseConflict, CaseCodeForStatus(409))
	require.Equal(t, CaseUnprocessable, CaseCodeForStatus(422))
	require.Equal(t, CaseRateLimited, CaseCodeForStatus(429))
	require.Equal(t, CaseInternalError, CaseCodeForStatus(500))
}
