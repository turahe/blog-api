package handlers

import (
	"context"
	"encoding/csv"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	nldomain "github.com/turahe/blog-api/internal/core/newsletter/domain"
	nlservice "github.com/turahe/blog-api/internal/core/newsletter/service"
)

// Subscriber CSV export paging.
const (
	newsletterExportPage    = 100
	newsletterExportMaxRows = 50000
)

type newsletterAdminAPI interface {
	ListSubscribers(ctx context.Context, filter nldomain.SubscriberFilter) (nldomain.SubscriberPage, error)
	Subscriber(ctx context.Context, id uuid.UUID) (nlservice.SubscriberDetail, error)
	DeleteSubscriber(ctx context.Context, actor, id uuid.UUID, mode string) (nlservice.SubscriberDetail, error)
	CreateIssue(ctx context.Context, actor uuid.UUID, in nlservice.IssueInput) (nldomain.Issue, error)
	UpdateIssue(ctx context.Context, actor, id uuid.UUID, patch nlservice.IssuePatch) (nldomain.Issue, error)
	Issue(ctx context.Context, id uuid.UUID) (nldomain.Issue, error)
	ListIssues(ctx context.Context, filter nldomain.IssueFilter) (nldomain.IssuePage, error)
	Preview(ctx context.Context, issue nldomain.Issue) (html, text string, err error)
	ProviderConfig(ctx context.Context) (nlservice.ProviderConfig, error)
	SaveProviderConfig(ctx context.Context, actor uuid.UUID, cfg nldomain.Config, lists []nldomain.ListInput) (nlservice.ProviderConfig, error)
}

// wireNewsletterAdmin binds the admin newsletter handlers. Editors read and export subscribers
// and write and send issues; erasing subscribers and the provider config are admin-only.
func wireNewsletterAdmin(c *routes.Newsletter, deps Deps, nl newsletterAdminAPI) {
	canExport := func(c *gin.Context) bool { return holds(c, deps, nldomain.PermSubscribersExport, editorRoles) }
	canSend := func(c *gin.Context) bool { return holds(c, deps, nldomain.PermIssuesSend, editorRoles) }
	provider := deps.NewsletterProvider

	c.AdminSubscribersList = gate(deps, nldomain.PermSubscribersRead, editorRoles, adminNewsletterSubscribersHandler(nl, canExport))
	c.AdminSubscriberGet = gate(deps, nldomain.PermSubscribersRead, editorRoles, adminNewsletterSubscriberHandler(nl))
	c.AdminSubscriberDelete = gate(deps, nldomain.PermSubscribersErase, adminRoles, adminNewsletterDeleteSubscriberHandler(nl))
	c.AdminIssuesList = gate(deps, nldomain.PermIssuesRead, editorRoles, adminNewsletterIssuesHandler(nl))
	c.AdminIssueCreate = gate(deps, nldomain.PermIssuesEdit, editorRoles, adminNewsletterCreateIssueHandler(nl, canSend))
	c.AdminIssueGet = gate(deps, nldomain.PermIssuesRead, editorRoles, adminNewsletterIssueHandler(nl))
	c.AdminIssuePatch = gate(deps, nldomain.PermIssuesEdit, editorRoles, adminNewsletterUpdateIssueHandler(nl, canSend))
	c.AdminConfigGet = gate(deps, nldomain.PermConfigRead, adminRoles, adminNewsletterConfigHandler(nl, provider))
	c.AdminConfigPut = gate(deps, nldomain.PermConfigUpdate, adminRoles, adminNewsletterSaveConfigHandler(nl, provider))
}

// adminNewsletterSubscribersHandler godoc
//
//	@Summary		List newsletter subscribers
//	@Description	Filters by status, list slug, and email prefix (q). format=csv downloads every matching
//	@Description	subscriber (up to 50000) as CSV and needs newsletter.subscribers.export.
//	@Tags			admin
//	@Produce		json
//	@Produce		text/csv
//	@Param			status		query		string	false	"pending_confirm, active, unsubscribed, bounced, complained, erased"
//	@Param			list		query		string	false	"list slug"
//	@Param			q			query		string	false	"email prefix"
//	@Param			format		query		string	false	"json or csv"	Enums(json, csv)	default(json)
//	@Param			page		query		int		false	"page"			default(1)
//	@Param			per_page	query		int		false	"per page"		default(20)
//	@Success		200			{object}	responses.Envelope
//	@Failure		400			{object}	responses.Envelope
//	@Failure		401			{object}	responses.Envelope
//	@Failure		403			{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/newsletter/subscribers [get]
func adminNewsletterSubscribersHandler(nl newsletterAdminAPI, canExport func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter := nldomain.SubscriberFilter{
			Status: nldomain.Status(strings.TrimSpace(c.Query("status"))),
			List:   strings.TrimSpace(c.Query("list")),
			Query:  strings.TrimSpace(c.Query("q")),
		}

		switch c.DefaultQuery("format", "json") {
		case "json":
		case "csv":
			if !canExport(c) {
				responses.FailureFor(c, nethttp.StatusForbidden, responses.FailureOpts{
					Service: responses.ServiceNewsletter, Code: "rbac.forbidden", Message: "Insufficient permissions",
				})

				return
			}

			exportNewsletterSubscribers(c, nl, filter)

			return
		default:
			newsletterValidation(c, "format must be json or csv")
			return
		}

		filter.Page, filter.PerPage = pageParams(c)

		page, err := nl.ListSubscribers(c.Request.Context(), filter)
		if writeNewsletterError(c, err) {
			return
		}

		items := make([]gin.H, 0, len(page.Items))
		for _, sub := range page.Items {
			items = append(items, responses.NewsletterSubscriber(sub))
		}

		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
			Service: responses.ServiceNewsletter, Data: items, Page: page.Page, PerPage: page.PerPage, Total: page.Total,
		})
	}
}

// exportNewsletterSubscribers streams the matching subscribers as CSV. The first page is read
// before any output so a bad filter still gets a JSON error.
func exportNewsletterSubscribers(c *gin.Context, nl newsletterAdminAPI, filter nldomain.SubscriberFilter) {
	filter.Page, filter.PerPage = 1, newsletterExportPage

	page, err := nl.ListSubscribers(c.Request.Context(), filter)
	if writeNewsletterError(c, err) {
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="newsletter-subscribers.csv"`)
	c.Header("Cache-Control", "no-store")
	c.Status(nethttp.StatusOK)

	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{
		"id", "email", "display_name", "status", "format", "source", "lists", "opted_in_at", "unsubscribed_at", "created_at",
	})

	for written := 0; ; {
		for _, sub := range page.Items {
			_ = w.Write(csvSafe([]string{
				sub.UUID.String(), sub.Email, sub.DisplayName, string(sub.Status), string(sub.Format), string(sub.Source),
				strings.Join(sub.ActiveLists(), " "), csvTime(sub.OptedInAt), csvTime(sub.UnsubscribedAt),
				sub.CreatedAt.UTC().Format(time.RFC3339),
			}))
		}

		written += len(page.Items)
		if len(page.Items) < newsletterExportPage || written >= newsletterExportMaxRows {
			break
		}

		filter.Page++

		if page, err = nl.ListSubscribers(c.Request.Context(), filter); err != nil {
			responses.RecordError(c, err)
			break
		}
	}

	w.Flush()
}

// csvSafe prefixes cells a spreadsheet would run as a formula.
func csvSafe(cells []string) []string {
	for i, cell := range cells {
		if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
			cells[i] = "'" + cell
		}
	}

	return cells
}

func csvTime(t *time.Time) string {
	if t == nil {
		return ""
	}

	return t.UTC().Format(time.RFC3339)
}

// adminNewsletterSubscriberHandler godoc
//
//	@Summary		Get a newsletter subscriber
//	@Description	The subscriber, list memberships, and the latest 100 consent events.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"subscriber UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/newsletter/subscribers/{param1} [get]
func adminNewsletterSubscriberHandler(nl newsletterAdminAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := newsletterPathID(c)
		if !ok {
			return
		}

		detail, err := nl.Subscriber(c.Request.Context(), id)
		if writeNewsletterError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess,
			responses.NewsletterSubscriberDetail(detail.Subscriber, detail.History))
	}
}

// adminNewsletterDeleteSubscriberHandler godoc
//
//	@Summary		Unsubscribe or erase a newsletter subscriber
//	@Description	mode=unsubscribe (default) stops all mail. mode=hard_delete erases the address and name;
//	@Description	the row stays, suppressed, with its consent history, so the address is never mailed again
//	@Description	without a new opt-in.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"subscriber UUID"
//	@Param			mode	query		string	false	"unsubscribe or hard_delete"	Enums(unsubscribe, hard_delete)	default(unsubscribe)
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/newsletter/subscribers/{param1} [delete]
func adminNewsletterDeleteSubscriberHandler(nl newsletterAdminAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := currentUser(c)
		if !ok {
			return
		}

		id, ok := newsletterPathID(c)
		if !ok {
			return
		}

		detail, err := nl.DeleteSubscriber(c.Request.Context(), actor, id, c.Query("mode"))
		if writeNewsletterError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess,
			responses.NewsletterSubscriberDetail(detail.Subscriber, detail.History))
	}
}

// adminNewsletterIssuesHandler godoc
//
//	@Summary	List newsletter issues
//	@Tags		admin
//	@Produce	json
//	@Param		status		query		string	false	"draft, scheduled, queued, sending, sent, cancelled"
//	@Param		page		query		int		false	"page"		default(1)
//	@Param		per_page	query		int		false	"per page"	default(20)
//	@Success	200			{object}	responses.Envelope
//	@Failure	400			{object}	responses.Envelope
//	@Failure	401			{object}	responses.Envelope
//	@Failure	403			{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/newsletter/issues [get]
func adminNewsletterIssuesHandler(nl newsletterAdminAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter := nldomain.IssueFilter{Status: nldomain.IssueStatus(strings.TrimSpace(c.Query("status")))}
		filter.Page, filter.PerPage = pageParams(c)

		page, err := nl.ListIssues(c.Request.Context(), filter)
		if writeNewsletterError(c, err) {
			return
		}

		items := make([]gin.H, 0, len(page.Items))
		for _, issue := range page.Items {
			items = append(items, responses.NewsletterIssue(issue, false))
		}

		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
			Service: responses.ServiceNewsletter, Data: items, Page: page.Page, PerPage: page.PerPage, Total: page.Total,
		})
	}
}

// adminNewsletterCreateIssueHandler godoc
//
//	@Summary		Create, schedule, or send a newsletter issue
//	@Description	status draft (default) saves; scheduled sends at send_at; queued sends now. Scheduling and
//	@Description	sending need newsletter.issues.send and a postal_address in the provider config. The Markdown
//	@Description	body is rendered to sanitized HTML and plain text inside a template that adds the unsubscribe
//	@Description	and preferences links and the postal address.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.NewsletterIssueCreate	true	"issue"
//	@Success		201		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		422		{object}	responses.Envelope	"newsletter.not_configured"
//	@Security		Bearer
//	@Router			/api/v1/admin/newsletter/issues [post]
func adminNewsletterCreateIssueHandler(nl newsletterAdminAPI, canSend func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := currentUser(c)
		if !ok {
			return
		}

		var body requests.NewsletterIssueCreate
		if !requests.BindJSON(c, &body) {
			return
		}

		status := nldomain.IssueStatus(body.Status)
		if !mayMoveIssue(c, status, canSend) {
			return
		}

		issue, err := nl.CreateIssue(c.Request.Context(), actor, nlservice.IssueInput{
			Subject: body.Subject, Preheader: body.Preheader, BodyMarkdown: body.BodyMarkdown,
			Lists: body.Lists, Status: status, SendAt: body.SendAt,
		})
		if writeNewsletterError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusCreated, responses.ServiceNewsletter, responses.CaseSuccess,
			responses.NewsletterIssue(issue, true))
	}
}

// adminNewsletterIssueHandler godoc
//
//	@Summary		Get a newsletter issue
//	@Description	preview=true adds the rendered html and text as a subscriber would receive them.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"issue UUID"
//	@Param			preview	query		bool	false	"include the rendered email"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/newsletter/issues/{param1} [get]
func adminNewsletterIssueHandler(nl newsletterAdminAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := newsletterPathID(c)
		if !ok {
			return
		}

		issue, err := nl.Issue(c.Request.Context(), id)
		if writeNewsletterError(c, err) {
			return
		}

		data := responses.NewsletterIssue(issue, true)

		if c.Query("preview") == "true" {
			html, text, err := nl.Preview(c.Request.Context(), issue)
			if writeNewsletterError(c, err) {
				return
			}

			data["preview"] = gin.H{"html": html, "text": text}
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess, data)
	}
}

// adminNewsletterUpdateIssueHandler godoc
//
//	@Summary		Update, schedule, send, or cancel a newsletter issue
//	@Description	Content and lists change only while draft or scheduled. status moves draft/scheduled to
//	@Description	draft, scheduled, queued, or cancelled; queued to cancelled; sending to queued (resume) or
//	@Description	cancelled. Scheduling and sending need newsletter.issues.send. 409 when the move is not
//	@Description	allowed or the issue changed meanwhile.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string							true	"issue UUID"
//	@Param			body	body		requests.NewsletterIssuePatch	true	"changes"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope	"newsletter.conflict"
//	@Failure		422		{object}	responses.Envelope	"newsletter.not_configured"
//	@Security		Bearer
//	@Router			/api/v1/admin/newsletter/issues/{param1} [patch]
func adminNewsletterUpdateIssueHandler(nl newsletterAdminAPI, canSend func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := currentUser(c)
		if !ok {
			return
		}

		id, ok := newsletterPathID(c)
		if !ok {
			return
		}

		var body requests.NewsletterIssuePatch
		if !requests.BindJSON(c, &body) {
			return
		}

		patch := nlservice.IssuePatch{
			Subject: body.Subject, Preheader: body.Preheader, BodyMarkdown: body.BodyMarkdown, Lists: body.Lists, SendAt: body.SendAt,
		}

		if body.Status != nil {
			status := nldomain.IssueStatus(*body.Status)
			if !mayMoveIssue(c, status, canSend) {
				return
			}

			patch.Status = &status
		}

		issue, err := nl.UpdateIssue(c.Request.Context(), actor, id, patch)
		if writeNewsletterError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess,
			responses.NewsletterIssue(issue, true))
	}
}

// mayMoveIssue rejects scheduling or sending without newsletter.issues.send.
func mayMoveIssue(c *gin.Context, status nldomain.IssueStatus, canSend func(*gin.Context) bool) bool {
	if status != nldomain.IssueScheduled && status != nldomain.IssueQueued {
		return true
	}

	if canSend(c) {
		return true
	}

	responses.FailureFor(c, nethttp.StatusForbidden, responses.FailureOpts{
		Service: responses.ServiceNewsletter, Code: "rbac.forbidden", Message: "Scheduling or sending needs newsletter.issues.send",
	})

	return false
}

// adminNewsletterConfigHandler godoc
//
//	@Summary		Get the newsletter provider config
//	@Description	Stored sending settings and lists, and the read-only provider from the environment
//	@Description	(NEWSLETTER_PROVIDER); the HMAC secret is never returned, only whether it is set.
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	responses.Envelope
//	@Failure		401	{object}	responses.Envelope
//	@Failure		403	{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/newsletter/provider-config [get]
func adminNewsletterConfigHandler(nl newsletterAdminAPI, provider responses.NewsletterProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg, err := nl.ProviderConfig(c.Request.Context())
		if writeNewsletterError(c, err) {
			return
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess,
			responses.NewsletterProviderConfig(cfg.Config, cfg.Lists, provider))
	}
}

// adminNewsletterSaveConfigHandler godoc
//
//	@Summary		Replace the newsletter provider config
//	@Description	Saves the sending settings and the complete list set. Lists missing from lists are archived:
//	@Description	they keep memberships and history but can no longer be joined or targeted. At least one list
//	@Description	must be a default list. The provider, endpoint, and secret are environment settings.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.NewsletterProviderConfig	true	"settings and lists"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/newsletter/provider-config [put]
func adminNewsletterSaveConfigHandler(nl newsletterAdminAPI, provider responses.NewsletterProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := currentUser(c)
		if !ok {
			return
		}

		var body requests.NewsletterProviderConfig
		if !requests.BindJSON(c, &body) {
			return
		}

		lists := make([]nldomain.ListInput, 0, len(body.Lists))
		for _, l := range body.Lists {
			lists = append(lists, nldomain.ListInput{Slug: l.Slug, Name: l.Name, Description: l.Description, IsDefault: l.IsDefault})
		}

		saved, err := nl.SaveProviderConfig(c.Request.Context(), actor, nldomain.Config{
			FromName: body.FromName, FromEmail: body.FromEmail, ReplyTo: body.ReplyTo, PostalAddress: body.PostalAddress,
			ConfirmTTL: time.Duration(body.ConfirmTTLHours) * time.Hour, DoubleOptInRequired: *body.DoubleOptInRequired,
		}, lists)
		if writeNewsletterError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceNewsletter, responses.CaseSuccess,
			responses.NewsletterProviderConfig(saved.Config, saved.Lists, provider))
	}
}

func newsletterPathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
	if err != nil {
		newsletterValidation(c, "Invalid id")
		return uuid.Nil, false
	}

	return id, true
}
