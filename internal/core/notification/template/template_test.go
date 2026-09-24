package template_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/notification/template"
)

func TestPasswordResetKeepsTokenInEmailOnly(t *testing.T) {
	t.Parallel()

	data := template.Data{
		Name: "Ada", Email: "ada@example.com", Token: "secret-token",
		ExpiresAt: "2026-09-25T00:00:00Z", PublicURL: "http://127.0.0.1:8080",
	}

	email, err := template.Render(template.ChannelEmail, template.TypePasswordReset, data)
	require.NoError(t, err)
	require.Equal(t, "Reset your password", email.Subject)
	require.Contains(t, email.Body, "secret-token")
	require.NotContains(t, email.Subject, "secret-token")

	for _, channel := range []template.Channel{template.ChannelWeb, template.ChannelSSE} {
		msg, err := template.Render(channel, template.TypePasswordReset, data)
		require.NoError(t, err)
		require.NotContains(t, msg.Title+msg.Body+msg.Preview, "secret-token")
	}
}

func TestSSEEventName(t *testing.T) {
	t.Parallel()

	msg, err := template.Render(template.ChannelSSE, template.TypePublicationPublished, template.Data{
		Name: "Ada", PostTitle: "Launch notes", PostURL: "http://127.0.0.1:8080/posts/launch",
	})
	require.NoError(t, err)
	require.Equal(t, "notification.created", msg.Event)
	require.Equal(t, "Launch notes", msg.Preview)
	require.NotContains(t, msg.Preview, "\n")
}

type memStore struct {
	rows map[string]template.Template
	err  error
}

func (m *memStore) Get(_ context.Context, typ string, channel template.Channel) (template.Template, error) {
	if m.err != nil {
		return template.Template{}, m.err
	}

	tpl, ok := m.rows[typ+"/"+string(channel)]
	if !ok {
		return template.Template{}, template.ErrNotFound
	}

	return tpl, nil
}

func (m *memStore) List(context.Context) ([]template.Template, error) { return nil, nil }

func (m *memStore) Save(_ context.Context, tpl template.Template) error {
	m.rows[tpl.Type+"/"+string(tpl.Channel)] = tpl
	return nil
}

func (m *memStore) SeedDefaults(context.Context, []template.Template) error { return nil }

func TestRendererUsesStoredTemplate(t *testing.T) {
	t.Parallel()

	store := &memStore{rows: map[string]template.Template{}}
	require.NoError(t, store.Save(context.Background(), template.Template{
		Type: template.TypePasswordChanged, Channel: template.ChannelEmail,
		Subject: "Custom: password updated", Body: "Hello {{.Name}}",
	}))

	msg, err := template.NewRenderer(store, nil).Render(context.Background(), template.ChannelEmail,
		template.TypePasswordChanged, template.Data{Name: "Ada"})
	require.NoError(t, err)
	require.Equal(t, "Custom: password updated", msg.Subject)
	require.Equal(t, "Hello Ada", msg.Body)
}

func TestRendererFallsBackToBuiltIn(t *testing.T) {
	t.Parallel()

	for _, store := range []*memStore{
		{rows: map[string]template.Template{}},
		{err: errors.New("database down")},
	} {
		msg, err := template.NewRenderer(store, nil).Render(context.Background(), template.ChannelEmail,
			template.TypePasswordChanged, template.Data{Name: "Ada", Email: "ada@example.com"})
		require.NoError(t, err)
		require.Equal(t, "Your password was changed", msg.Subject)
	}
}

func TestStoredTemplateCannotLeakToken(t *testing.T) {
	t.Parallel()

	leaky := template.Template{
		Type: template.TypePasswordReset, Channel: template.ChannelWeb,
		Title: `{{printf "%v" .}}`, Body: `{{printf "%v" .}}`,
	}

	msg, err := template.RenderTemplate(leaky, template.Data{Name: "Ada", Token: "secret-token"})
	require.NoError(t, err)
	require.NotContains(t, msg.Title+msg.Body, "secret-token")
}

func TestValidate(t *testing.T) {
	t.Parallel()

	for _, tpl := range template.Defaults() {
		require.NoError(t, template.Validate(tpl), "%s/%s", tpl.Type, tpl.Channel)
	}

	cases := map[string]template.Template{
		"token in web body":  {Type: "x", Channel: template.ChannelWeb, Title: "t", Body: "{{ .Token }}"},
		"token in subject":   {Type: "x", Channel: template.ChannelEmail, Subject: "{{.Token}}", Body: "b"},
		"unknown field":      {Type: "x", Channel: template.ChannelEmail, Subject: "s", Body: "{{.Password}}"},
		"unknown channel":    {Type: "x", Channel: "push", Title: "t"},
		"sse without event":  {Type: "x", Channel: template.ChannelSSE, Title: "t"},
		"email without subj": {Type: "x", Channel: template.ChannelEmail, Body: "b"},
	}
	for name, tpl := range cases {
		require.Error(t, template.Validate(tpl), name)
	}
}

func TestStripsNewlinesFromValues(t *testing.T) {
	t.Parallel()

	msg, err := template.Render(template.ChannelWeb, template.TypeModerationAlert, template.Data{
		Name: "Ada", ActorName: "Eve\r\nBcc: evil@example.com", PostTitle: "Hello", Reason: "spam",
	})
	require.NoError(t, err)
	require.NotContains(t, msg.Body, "\n")
	require.Contains(t, msg.Body, "Eve Bcc: evil@example.com")
}
