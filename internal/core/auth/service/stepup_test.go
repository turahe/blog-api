package service_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

func TestIssueAccessDelegatesToTokenService(t *testing.T) {
	t.Parallel()

	users, sessions, resets := newMemStores()
	subject := uuid.New()

	raw, err := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil).
		IssueAccess(authdomain.AccessClaims{Subject: subject})
	require.NoError(t, err)
	assert.Equal(t, "access:"+subject.String()+":"+uuid.Nil.String(), raw)

	_, err = newServiceAt(users, sessions, resets, fakeHasher{}, failingTokens{fail: faults{"IssueAccess": errBoom}}, nil).
		IssueAccess(authdomain.AccessClaims{Subject: subject})
	require.ErrorIs(t, err, errBoom)
}

func TestVerifyStepUpWithoutTwoFactorConfiguredNeedsOnlyPassword(t *testing.T) {
	t.Parallel()

	user := activeTestUser()
	users, sessions, resets := newMemStores(user)
	svc := newServiceAt(users, sessions, resets, fakeHasher{}, fakeTokens{}, nil)

	ok, err := svc.VerifyStepUp(t.Context(), user.UUID, oldPassword, "")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestVerifyStepUpPropagatesLookupFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(f *twoFactorFixture)
	}{
		{name: "account lookup", setup: func(f *twoFactorFixture) { f.users.fail = faults{"FindByID": errBoom} }},
		{name: "enrollment lookup", setup: func(f *twoFactorFixture) { f.repo.fail = faults{"Find": errBoom} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newTwoFactorFixture(t)
			f.enroll(t)
			code := f.code(t)
			tt.setup(f)

			ok, err := f.svc.VerifyStepUp(t.Context(), f.userID, tfPassword, code)
			require.ErrorIs(t, err, errBoom)
			assert.False(t, ok)
		})
	}
}

func TestVerifyStepUpWithoutTwoFactorNeedsOnlyPassword(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)

	ok, err := f.svc.VerifyStepUp(t.Context(), f.userID, tfPassword, "")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = f.svc.VerifyStepUp(t.Context(), f.userID, "wrong", "")
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = f.svc.VerifyStepUp(t.Context(), uuid.New(), tfPassword, "")
	require.NoError(t, err)
	assert.False(t, ok, "an unknown account never passes")
}

func TestVerifyStepUpWithTwoFactorNeedsPasswordAndCode(t *testing.T) {
	t.Parallel()

	f := newTwoFactorFixture(t)
	backup := f.enroll(t)

	for name, tc := range map[string]struct{ password, code string }{
		"password only":  {tfPassword, ""},
		"wrong code":     {tfPassword, "000000"},
		"wrong password": {"wrong", f.code(t)},
	} {
		ok, err := f.svc.VerifyStepUp(t.Context(), f.userID, tc.password, tc.code)
		require.NoError(t, err, name)
		assert.False(t, ok, name)
	}

	ok, err := f.svc.VerifyStepUp(t.Context(), f.userID, tfPassword, f.code(t))
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = f.svc.VerifyStepUp(t.Context(), f.userID, tfPassword, backup[0])
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = f.svc.VerifyStepUp(t.Context(), f.userID, tfPassword, backup[0])
	require.NoError(t, err)
	assert.False(t, ok, "a backup code is consumed")
}
