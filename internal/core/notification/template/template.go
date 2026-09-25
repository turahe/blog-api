// Package template renders account notifications for email, web, and SSE.
// Templates are stored in notification_templates; the built-in catalogue is the
// seed and the fallback. Secrets such as reset tokens are allowed only in the
// email body.
package template

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"text/template"
)

// Channel is a delivery surface.
type Channel string

// Delivery channels.
const (
	ChannelEmail Channel = "email"
	ChannelWeb   Channel = "web"
	ChannelSSE   Channel = "sse"
)

// eventNotificationCreated is the default SSE event name.
const eventNotificationCreated = "notification.created"

// Types are stable notification identifiers.
const (
	TypeAccountVerify = "account.verify"
	// TypeAccountExists tells an account holder that someone tried to register their address.
	// It is email only.
	TypeAccountExists        = "account.exists"
	TypePasswordReset        = "password.reset"
	TypePasswordChanged      = "password.changed"
	TypeEmailChangeConfirm   = "email.change.confirm"
	TypeEmailChangeNotice    = "email.change.notice"
	TypeEmailChanged         = "email.changed"
	TypeModerationAlert      = "moderation.alert"
	TypePublicationPublished = "publication.published"
	// TypePublicationRepublished tells commenters that a post they discussed is public again.
	TypePublicationRepublished = "publication.republished"
	TypeCommentReply           = "comment.reply"
	TypeCommentModerated       = "comment.moderated"
	// TypeNewsletterConfirm carries the double opt-in link; TypeNewsletterWelcome follows the
	// confirmation with a preferences link. Both are email only.
	TypeNewsletterConfirm = "newsletter.confirm"
	TypeNewsletterWelcome = "newsletter.welcome"
)

// Data is the plain-text values substituted into a template.
type Data struct {
	Name      string
	Email     string
	NewEmail  string
	Token     string
	ExpiresAt string
	PublicURL string
	PostTitle string
	PostURL   string
	Reason    string
	ActorName string
	// Excerpt is a short plain-text quote of user content, such as a reply.
	Excerpt string
	// Outcome is a past-tense moderation result, such as "approved" or "marked as spam".
	Outcome string
	// SiteURL is the public site origin that hosts pages such as /newsletter/confirm.
	SiteURL  string
	SiteName string
	// Lists names newsletter lists, comma separated.
	Lists string
}

// Message is one rendered channel payload.
type Message struct {
	Channel Channel
	Type    string
	Subject string
	Title   string
	Body    string
	Preview string
	Event   string
}

// Template is one stored template for a type and channel.
type Template struct {
	Type    string
	Channel Channel
	Subject string
	Title   string
	Body    string
	Preview string
	Event   string
}

// ErrNotFound is returned by a Store when no row exists for a type and channel.
var ErrNotFound = errors.New("notification template not found")

// Store loads and saves templates.
type Store interface {
	Get(ctx context.Context, typ string, channel Channel) (Template, error)
	List(ctx context.Context) ([]Template, error)
	Save(ctx context.Context, tpl Template) error
	// SeedDefaults inserts the given templates, leaving existing rows unchanged.
	SeedDefaults(ctx context.Context, tpls []Template) error
}

type spec struct {
	subject string
	title   string
	body    string
	preview string
	event   string
}

// Renderer renders templates from a Store, falling back to the built-in catalogue
// when the store has no row or is unavailable.
type Renderer struct {
	store  Store
	logger *slog.Logger
}

// NewRenderer returns a Renderer. A nil store renders only built-in templates.
func NewRenderer(store Store, logger *slog.Logger) *Renderer {
	if logger == nil {
		logger = slog.Default()
	}

	return &Renderer{store: store, logger: logger}
}

// Render fills the stored template for one type and channel.
func (r *Renderer) Render(ctx context.Context, channel Channel, typ string, data Data) (Message, error) {
	if r == nil || r.store == nil {
		return Render(channel, typ, data)
	}

	tpl, err := r.store.Get(ctx, typ, channel)
	if errors.Is(err, ErrNotFound) {
		return Render(channel, typ, data)
	}

	if err != nil {
		r.logger.WarnContext(ctx, "notify: template store unavailable, using built-in copy",
			"type", typ, "channel", channel, "error", err)

		return Render(channel, typ, data)
	}

	return RenderTemplate(tpl, data)
}

// Render fills the built-in template for one type and channel.
func Render(channel Channel, typ string, data Data) (Message, error) {
	tpl, err := builtin(typ, channel)
	if err != nil {
		return Message{}, err
	}

	return RenderTemplate(tpl, data)
}

// RenderTemplate fills tpl with data. The token is visible only to the email body,
// so a stored template cannot leak it through other fields or channels.
func RenderTemplate(tpl Template, data Data) (Message, error) {
	safe := scrub(data)
	public := safe
	public.Token = ""

	bodyData := public
	if tpl.Channel == ChannelEmail {
		bodyData = safe
	}

	subject, err := fill(tpl.Subject, public)
	if err != nil {
		return Message{}, err
	}

	title, err := fill(tpl.Title, public)
	if err != nil {
		return Message{}, err
	}

	body, err := fill(tpl.Body, bodyData)
	if err != nil {
		return Message{}, err
	}

	preview, err := fill(tpl.Preview, public)
	if err != nil {
		return Message{}, err
	}

	return Message{
		Channel: tpl.Channel,
		Type:    tpl.Type,
		Subject: oneLine(subject),
		Title:   oneLine(title),
		Body:    body,
		Preview: oneLine(preview),
		Event:   tpl.Event,
	}, nil
}

// Validate checks a template before it is saved.
func Validate(tpl Template) error {
	switch tpl.Channel {
	case ChannelEmail, ChannelWeb, ChannelSSE:
	default:
		return fmt.Errorf("unknown channel %q", tpl.Channel)
	}

	if strings.TrimSpace(tpl.Type) == "" {
		return errors.New("type is required")
	}

	if tpl.Channel == ChannelEmail && strings.TrimSpace(tpl.Subject) == "" {
		return errors.New("email templates need a subject")
	}

	if tpl.Channel != ChannelEmail && strings.TrimSpace(tpl.Title) == "" {
		return errors.New("web and sse templates need a title")
	}

	if tpl.Channel == ChannelSSE && strings.TrimSpace(tpl.Event) == "" {
		return errors.New("sse templates need an event name")
	}

	return validateContent(tpl)
}

// validateContent keeps secrets out of non-email surfaces and checks the patterns parse.
func validateContent(tpl Template) error {
	for field, value := range map[string]string{
		"subject": tpl.Subject, "title": tpl.Title, "preview": tpl.Preview,
	} {
		if mentionsToken(value) {
			return fmt.Errorf("{{.Token}} is not allowed in %s", field)
		}
	}

	if tpl.Channel != ChannelEmail && mentionsToken(tpl.Body) {
		return errors.New("{{.Token}} is allowed only in email bodies")
	}

	for _, pattern := range []string{tpl.Subject, tpl.Title, tpl.Body, tpl.Preview} {
		if _, err := fill(pattern, Data{}); err != nil {
			return fmt.Errorf("invalid template: %w", err)
		}
	}

	return nil
}

// Defaults returns the built-in catalogue as rows, sorted by type then channel.
func Defaults() []Template {
	out := make([]Template, 0, len(catalogue)*3)
	for typ, byChannel := range catalogue {
		for channel, item := range byChannel {
			out = append(out, toTemplate(typ, channel, item))
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}

		return out[i].Channel < out[j].Channel
	})

	return out
}

func builtin(typ string, channel Channel) (Template, error) {
	byChannel, ok := catalogue[typ]
	if !ok {
		return Template{}, fmt.Errorf("unknown notification type %q", typ)
	}

	item, ok := byChannel[channel]
	if !ok {
		return Template{}, fmt.Errorf("notification %q has no %s template", typ, channel)
	}

	return toTemplate(typ, channel, item), nil
}

func toTemplate(typ string, channel Channel, item spec) Template {
	return Template{
		Type: typ, Channel: channel,
		Subject: item.subject, Title: item.title, Body: item.body,
		Preview: item.preview, Event: item.event,
	}
}

// mentionsToken reports whether a pattern reads the Token field, ignoring spacing.
func mentionsToken(pattern string) bool {
	return strings.Contains(strings.Join(strings.Fields(pattern), ""), ".Token")
}

func fill(pattern string, data Data) (string, error) {
	if pattern == "" {
		return "", nil
	}

	tpl, err := template.New("n").Option("missingkey=error").Parse(pattern)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := tpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func scrub(data Data) Data {
	data.Name = oneLine(data.Name)
	data.Email = oneLine(data.Email)
	data.NewEmail = oneLine(data.NewEmail)
	data.Token = oneLine(data.Token)
	data.ExpiresAt = oneLine(data.ExpiresAt)
	data.PublicURL = oneLine(data.PublicURL)
	data.PostTitle = oneLine(data.PostTitle)
	data.PostURL = oneLine(data.PostURL)
	data.Reason = oneLine(data.Reason)
	data.ActorName = oneLine(data.ActorName)
	data.Excerpt = oneLine(data.Excerpt)
	data.Outcome = oneLine(data.Outcome)
	data.SiteURL = oneLine(data.SiteURL)
	data.SiteName = oneLine(data.SiteName)
	data.Lists = oneLine(data.Lists)

	return data
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

var catalogue = map[string]map[Channel]spec{
	TypeAccountVerify: {
		ChannelEmail: {
			subject: "Verify your email address",
			body:    "Hi {{.Name}},\n\nConfirm {{.Email}} before {{.ExpiresAt}}.\n\nToken: {{.Token}}\n\nPOST {{.PublicURL}}/api/v1/auth/verify-email\n\nIf you did not create this account, ignore this message.\n",
		},
		ChannelWeb: {
			title: "Verify your email",
			body:  "A verification message was sent to your email. It expires at {{.ExpiresAt}}.",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "Verify your email",
			preview: "Check your email to confirm this account.",
		},
	},
	TypeAccountExists: {
		ChannelEmail: {
			subject: "You already have an account",
			body:    "Hi {{.Name}},\n\nSomeone tried to create an account with {{.Email}}, which already has one. Nothing was changed.\n\nIf it was you and you forgot your password, request a reset: POST {{.PublicURL}}/api/v1/auth/password/forgot\n\nIf it was not you, ignore this message.\n",
		},
	},
	TypePasswordReset: {
		ChannelEmail: {
			subject: "Reset your password",
			body:    "Hi {{.Name}},\n\nUse this token to reset the password for {{.Email}} before {{.ExpiresAt}}.\n\nToken: {{.Token}}\n\nPOST {{.PublicURL}}/api/v1/auth/password/reset\n\nIf you did not request this, ignore this message.\n",
		},
		ChannelWeb: {
			title: "Reset your password",
			body:  "A password reset was requested. The token was emailed to you and expires at {{.ExpiresAt}}.",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "Reset your password",
			preview: "Check your email for the reset token.",
		},
	},
	TypePasswordChanged: {
		ChannelEmail: {
			subject: "Your password was changed",
			body:    "Hi {{.Name}},\n\nThe password for {{.Email}} was just changed. If you did not do this, reset your password.\n",
		},
		ChannelWeb: {
			title: "Your password was changed",
			body:  "The password on your account was just updated. If this was not you, reset it.",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "Your password was changed",
			preview: "If this was not you, reset your password.",
		},
	},
	TypeEmailChangeConfirm: {
		ChannelEmail: {
			subject: "Confirm your new email address",
			body:    "Hi {{.Name}},\n\nConfirm {{.NewEmail}} as the address for your account before {{.ExpiresAt}}.\n\nToken: {{.Token}}\n\nPOST {{.PublicURL}}/api/v1/me/email/confirm-change with this token while signed in.\n\nIf you did not request this change, ignore this message.\n",
		},
		ChannelWeb: {
			title: "Confirm your new email",
			body:  "A confirmation message was sent to the new address. It expires at {{.ExpiresAt}}.",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "Confirm your new email",
			preview: "Check the new address for the confirmation token.",
		},
	},
	TypeEmailChangeNotice: {
		ChannelEmail: {
			subject: "Email change requested",
			body:    "Hi {{.Name}},\n\nSomeone requested to change the email on your account. The change is not complete until the new address confirms it.\n\nIf you did not request this, change your password.\n",
		},
		ChannelWeb: {
			title: "Email change requested",
			body:  "A change to your account email is waiting for confirmation. If this was not you, change your password.",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "Email change requested",
			preview: "Confirm it from the new address, or change your password if this was not you.",
		},
	},
	TypeEmailChanged: {
		ChannelEmail: {
			subject: "Your email address was changed",
			body:    "Hi {{.Name}},\n\nThe email on your account is now {{.NewEmail}}. All sessions were signed out.\n",
		},
		ChannelWeb: {
			title: "Your email address was changed",
			body:  "This account now uses {{.NewEmail}}. All sessions were signed out.",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "Your email address was changed",
			preview: "All sessions were signed out.",
		},
	},
	TypeModerationAlert: {
		ChannelEmail: {
			subject: "Moderation alert: {{.PostTitle}}",
			body:    "Hi {{.Name}},\n\n{{.ActorName}} flagged content on \"{{.PostTitle}}\".\n\nReason: {{.Reason}}\n\nOpen {{.PostURL}}\n",
		},
		ChannelWeb: {
			title: "Moderation alert",
			body:  "{{.ActorName}} flagged \"{{.PostTitle}}\". Reason: {{.Reason}}",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "Moderation alert",
			preview: "{{.PostTitle}} needs review.",
		},
	},
	TypePublicationPublished: {
		ChannelEmail: {
			subject: "Published: {{.PostTitle}}",
			body:    "Hi {{.Name}},\n\n\"{{.PostTitle}}\" is now published.\n\n{{.PostURL}}\n",
		},
		ChannelWeb: {
			title: "Your post is published",
			body:  "\"{{.PostTitle}}\" is now public.",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "Your post is published",
			preview: "{{.PostTitle}}",
		},
	},
	TypePublicationRepublished: {
		ChannelWeb: {
			title: "A post you commented on is back",
			body:  "\"{{.PostTitle}}\" is published again.",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "A post you commented on is back",
			preview: "{{.PostTitle}}",
		},
	},
	TypeCommentReply: {
		ChannelWeb: {
			title: "{{.ActorName}} replied to your comment",
			body:  "On \"{{.PostTitle}}\": {{.Excerpt}}",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "{{.ActorName}} replied to your comment",
			preview: "{{.Excerpt}}",
		},
	},
	TypeNewsletterConfirm: {
		ChannelEmail: {
			subject: "Confirm your subscription to {{.SiteName}}",
			body:    "Hi{{if .Name}} {{.Name}}{{end}},\n\nConfirm your subscription to {{.Lists}} from {{.SiteName}} before {{.ExpiresAt}}:\n\n{{.SiteURL}}/newsletter/confirm?token={{.Token}}\n\nIf you did not ask for this, ignore this message and you will not be subscribed.\n",
		},
	},
	TypeNewsletterWelcome: {
		ChannelEmail: {
			subject: "You're subscribed to {{.SiteName}}",
			body:    "Hi{{if .Name}} {{.Name}}{{end}},\n\nYou're now subscribed to {{.Lists}}.\n\nChange what you receive or unsubscribe at any time:\n\n{{.SiteURL}}/newsletter/preferences?token={{.Token}}\n",
		},
	},
	TypeCommentModerated: {
		ChannelWeb: {
			title: "Your comment was {{.Outcome}}",
			body:  "Your comment on \"{{.PostTitle}}\" was {{.Outcome}}.{{if .Reason}} Reason: {{.Reason}}{{end}}",
		},
		ChannelSSE: {
			event:   eventNotificationCreated,
			title:   "Your comment was {{.Outcome}}",
			preview: "{{.PostTitle}}",
		},
	},
}
