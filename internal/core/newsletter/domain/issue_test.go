package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
)

func TestIssueValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		edit    func(i *domain.Issue)
		wantErr error
	}{
		{name: "valid"},
		{name: "longest subject", edit: func(i *domain.Issue) { i.Subject = strings.Repeat("s", domain.MaxSubjectLength) }},
		{name: "subject too long", edit: func(i *domain.Issue) { i.Subject = strings.Repeat("s", domain.MaxSubjectLength+1) }, wantErr: domain.ErrValidation},
		{name: "blank subject", edit: func(i *domain.Issue) { i.Subject = "  " }, wantErr: domain.ErrValidation},
		{name: "preheader too long", edit: func(i *domain.Issue) { i.Preheader = strings.Repeat("p", domain.MaxSubjectLength+1) }, wantErr: domain.ErrValidation},
		{name: "preheader on two lines", edit: func(i *domain.Issue) { i.Preheader = "a\r\nb" }, wantErr: domain.ErrValidation},
		{name: "blank body", edit: func(i *domain.Issue) { i.BodyMarkdown = "\n" }, wantErr: domain.ErrValidation},
		{name: "body too long", edit: func(i *domain.Issue) { i.BodyMarkdown = strings.Repeat("b", domain.MaxBodyLength+1) }, wantErr: domain.ErrValidation},
		{name: "too many lists", edit: func(i *domain.Issue) { i.Lists = make([]string, domain.MaxLists+1) }, wantErr: domain.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			issue := domain.Issue{Subject: "Hello", Preheader: "Hi", BodyMarkdown: "Body", Lists: []string{"weekly"}}
			if tt.edit != nil {
				tt.edit(&issue)
			}

			err := issue.Validate()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}
