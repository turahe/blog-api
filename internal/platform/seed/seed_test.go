package seed

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptionsWithDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts Options
		want Options
	}{
		{
			name: "empty uses development defaults",
			want: Options{AdminEmail: "admin@example.com", AdminUsername: "admin", AdminPassword: "ChangeMeNow!123", AdminName: "Administrator"},
		},
		{
			name: "set fields are kept",
			opts: Options{AdminEmail: "root@example.test", AdminUsername: "root", AdminPassword: "secret", AdminName: "Root"},
			want: Options{AdminEmail: "root@example.test", AdminUsername: "root", AdminPassword: "secret", AdminName: "Root"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.opts.withDefaults())
		})
	}
}

// The handlers fall back to these role sets when no RBAC enforcer is wired, so the seeded
// grants must hand each permission to exactly the same roles.
func TestSeededGrantsMatchHandlerFallbackRoles(t *testing.T) {
	t.Parallel()

	admin := []string{roleAdmin}
	editors := []string{roleAdmin, roleEditor}
	authors := []string{roleAdmin, roleEditor, roleAuthor}

	want := map[string][]string{
		"settings.read":                     admin,
		"settings.update":                   admin,
		"settings.history.read":             admin,
		"impersonation.start":               admin,
		"newsletter.subscribers.erase":      admin,
		"newsletter.provider_config.read":   admin,
		"newsletter.provider_config.update": admin,
		"analytics.export":                  admin,
		permNewsletterSubscribersRead:       editors,
		permNewsletterSubscribersExport:     editors,
		permNewsletterIssuesRead:            editors,
		permNewsletterIssuesEdit:            editors,
		permNewsletterIssuesSend:            editors,
		permMediaUsageRead:                  editors,
		permAnalyticsRead:                   editors,
		permAnalyticsSearchRead:             editors,
		permAnalyticsRealtimeRead:           editors,
		permRevisionsViewAll:                editors,
		permRevisionsView:                   authors,
		permRevisionsRestore:                authors,
		permSEOView:                         authors,
		permSEOEdit:                         authors,
		permSlugEdit:                        authors,
	}

	for permission, roles := range want {
		var got []string

		for role, perms := range rolePermissions {
			if slices.Contains(perms, permission) {
				got = append(got, role)
			}
		}

		slices.Sort(got)

		expected := slices.Clone(roles)
		slices.Sort(expected)

		if !slices.Equal(got, expected) {
			t.Errorf("%s granted to %v, want %v", permission, got, expected)
		}
	}
}
