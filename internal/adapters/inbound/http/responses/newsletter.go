package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	nldomain "github.com/turahe/blog-api/internal/core/newsletter/domain"
)

// NewsletterProvider is the environment-configured delivery provider, shown read-only.
type NewsletterProvider struct {
	Name             string `json:"name"`
	Endpoint         string `json:"endpoint,omitempty"`
	SecretConfigured bool   `json:"secret_configured"`
	SendingEnabled   bool   `json:"sending_enabled"`
	WebhookEnabled   bool   `json:"webhook_enabled"`
}

// NewsletterList renders a list.
func NewsletterList(l nldomain.List) gin.H {
	return gin.H{
		"slug": l.Slug, "name": l.Name, "description": l.Description, "is_default": l.IsDefault,
		"position": l.Position, "archived_at": RFC3339(l.ArchivedAt),
	}
}

// NewsletterLists renders lists in order.
func NewsletterLists(lists []nldomain.List) []gin.H {
	out := make([]gin.H, 0, len(lists))
	for _, l := range lists {
		out = append(out, NewsletterList(l))
	}

	return out
}

func newsletterMemberships(sub nldomain.Subscriber) []gin.H {
	out := make([]gin.H, 0, len(sub.Memberships))
	for _, m := range sub.Memberships {
		out = append(out, gin.H{
			"slug": m.ListSlug, "name": m.ListName, "state": string(m.State),
			"joined_at": RFC3339(m.JoinedAt), "left_at": RFC3339(m.LeftAt),
		})
	}

	return out
}

// NewsletterStatus is the body of subscribe, resend, and unsubscribe answers.
func NewsletterStatus(status string) gin.H { return gin.H{"status": status} }

// NewsletterConfirmed renders a subscriber right after confirmation.
func NewsletterConfirmed(sub nldomain.Subscriber) gin.H {
	lists := sub.ActiveLists()
	if lists == nil {
		lists = []string{}
	}

	return gin.H{"status": string(sub.Status), "lists": lists, "opted_in_at": RFC3339(sub.OptedInAt)}
}

// NewsletterMine renders the caller's own subscription; found is false when the account has none.
func NewsletterMine(sub nldomain.Subscriber, lists []nldomain.List, found bool) gin.H {
	if !found {
		return gin.H{
			"subscribed": false, "status": nil, "format": nil, "memberships": []gin.H{},
			"available_lists": NewsletterLists(lists),
		}
	}

	data := NewsletterPreferences(sub, lists, false)
	data["subscribed"] = sub.Status == nldomain.StatusActive || sub.Status == nldomain.StatusPending

	return data
}

// NewsletterPreferences renders a subscription for its owner (preferences page or /me). The
// address is masked on the token page, where a forwarded link must not reveal it.
func NewsletterPreferences(sub nldomain.Subscriber, lists []nldomain.List, maskEmail bool) gin.H {
	email := sub.Email
	if maskEmail {
		email = nldomain.MaskEmail(sub.Email)
	}

	return gin.H{
		"email":           email,
		"status":          string(sub.Status),
		"format":          string(sub.Format),
		"memberships":     newsletterMemberships(sub),
		"available_lists": NewsletterLists(lists),
		"opted_in_at":     RFC3339(sub.OptedInAt),
		"unsubscribed_at": RFC3339(sub.UnsubscribedAt),
	}
}

// NewsletterSubscriber renders a subscriber for staff.
func NewsletterSubscriber(sub nldomain.Subscriber) gin.H {
	return gin.H{
		"id":              sub.UUID,
		"email":           sub.Email,
		"display_name":    sub.DisplayName,
		"user_id":         sub.UserID,
		"status":          string(sub.Status),
		"format":          string(sub.Format),
		"source":          string(sub.Source),
		"memberships":     newsletterMemberships(sub),
		"opted_in_at":     RFC3339(sub.OptedInAt),
		"unsubscribed_at": RFC3339(sub.UnsubscribedAt),
		"bounced_at":      RFC3339(sub.BouncedAt),
		"complained_at":   RFC3339(sub.ComplainedAt),
		"erased_at":       RFC3339(sub.ErasedAt),
		"created_at":      sub.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":      sub.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// NewsletterSubscriberDetail renders a subscriber with its consent history, newest first.
func NewsletterSubscriberDetail(sub nldomain.Subscriber, history []nldomain.ConsentEvent) gin.H {
	events := make([]gin.H, 0, len(history))
	for _, e := range history {
		events = append(events, gin.H{
			"event": e.Event, "list": e.ListSlug, "source": e.Source, "reason_code": e.ReasonCode,
			"feedback": e.Feedback, "occurred_at": e.OccurredAt.UTC().Format(time.RFC3339),
		})
	}

	data := NewsletterSubscriber(sub)
	data["consent_history"] = events

	return data
}

// NewsletterIssue renders an issue; body_markdown is included only when withBody is set.
func NewsletterIssue(i nldomain.Issue, withBody bool) gin.H {
	data := gin.H{
		"id":           i.UUID,
		"subject":      i.Subject,
		"preheader":    i.Preheader,
		"lists":        i.Lists,
		"status":       string(i.Status),
		"send_at":      RFC3339(i.SendAt),
		"queued_at":    RFC3339(i.QueuedAt),
		"started_at":   RFC3339(i.StartedAt),
		"completed_at": RFC3339(i.CompletedAt),
		"sent_count":   i.SentCount,
		"failed_count": i.FailedCount,
		"created_by":   i.CreatedBy,
		"updated_by":   i.UpdatedBy,
		"created_at":   i.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":   i.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if withBody {
		data["body_markdown"] = i.BodyMarkdown
	}

	return data
}

// NewsletterProviderConfig renders the stored settings, the lists, and the read-only provider.
func NewsletterProviderConfig(cfg nldomain.Config, lists []nldomain.List, provider NewsletterProvider) gin.H {
	return gin.H{
		"from_name":             cfg.FromName,
		"from_email":            cfg.FromEmail,
		"reply_to":              cfg.ReplyTo,
		"postal_address":        cfg.PostalAddress,
		"confirm_ttl_hours":     int(cfg.ConfirmTTL.Hours()),
		"double_optin_required": cfg.DoubleOptInRequired,
		"ready_to_send":         cfg.ReadyToSend() == nil,
		"updated_at":            RFC3339(cfg.UpdatedAt),
		"updated_by":            cfg.UpdatedBy,
		"lists":                 NewsletterLists(lists),
		"provider":              provider,
	}
}
