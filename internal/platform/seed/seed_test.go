package seed

import (
	"slices"
	"testing"
)

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
		permNewsletterSubscribersRead:       editors,
		permNewsletterSubscribersExport:     editors,
		permNewsletterIssuesRead:            editors,
		permNewsletterIssuesEdit:            editors,
		permNewsletterIssuesSend:            editors,
		permMediaUsageRead:                  editors,
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
