package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	impdomain "github.com/turahe/blog-api/internal/core/impersonation/domain"
	impservice "github.com/turahe/blog-api/internal/core/impersonation/service"
)

const impersonationStartPerMinute = 10

type impersonationAPI interface {
	Start(ctx context.Context, in impservice.StartInput) (impservice.Started, error)
	Stop(ctx context.Context, actorID uuid.UUID, sessionID *uuid.UUID) (impdomain.Session, error)
	Current(ctx context.Context, actorID uuid.UUID, sessionID *uuid.UUID) (impdomain.Session, bool, error)
	Verify(ctx context.Context, sessionID string, actorID, targetID uuid.UUID) error
}

// impersonationControllers wires the impersonation handlers when the service is present.
// Stop and current need only a signed-in caller, so an impersonator who lost the permission
// can still see and end their session.
func impersonationControllers(deps Deps) routes.Impersonation {
	imp := deps.Impersonation
	if imp == nil {
		return routes.Impersonation{}
	}

	limit := middleware.RateLimit(deps.RateLimiter, deps.Logger, "impersonation.start", impersonationStartPerMinute, time.Minute)

	return routes.Impersonation{
		Start:   gate(deps, impdomain.PermissionStart, adminRoles, chain(limit, adminStartImpersonationHandler(imp))),
		Stop:    adminStopImpersonationHandler(imp),
		Current: adminCurrentImpersonationHandler(imp),
	}
}

// adminStartImpersonationHandler godoc
//
//	@Summary		Start impersonating a user
//	@Description	Opens an impersonation session and returns a Bearer token that acts as the target until
//	@Description	expiresAt (IMPERSONATION_TTL, never renewed; no refresh token). Requires impersonation.start,
//	@Description	the caller's current password, and a TOTP or backup code when the caller has two-factor
//	@Description	enabled. The target must be active, must not be an administrator or able to impersonate, and
//	@Description	every permission the target has must be one the caller has. One active session per caller.
//	@Description	The session ends with the caller's own sign-in (logout, revocation, or refresh-session expiry).
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.StartImpersonation	true	"target, reason, and step-up proof"
//	@Success		201		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope	"unauthorized, impersonation.sign_in_required (refresh the token first)"
//	@Failure		403		{object}	responses.Envelope	"rbac.forbidden, impersonation.step_up_required, impersonation.forbidden_action"
//	@Failure		409		{object}	responses.Envelope	"impersonation.already_active"
//	@Failure		422		{object}	responses.Envelope	"impersonation.target_ineligible"
//	@Failure		429		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/impersonation/start [post]
func adminStartImpersonationHandler(imp impersonationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorID, ok := currentUser(c)
		if !ok {
			return
		}

		var body requests.StartImpersonation
		if !requests.BindJSON(c, &body) {
			return
		}

		started, err := imp.Start(c.Request.Context(), impservice.StartInput{
			ActorID: actorID, TargetID: uuid.MustParse(body.TargetUserID), Reason: body.Reason,
			BaseFamilyID: middleware.CurrentSignInFamily(c),
			Password:     body.CurrentPassword, Code: body.TwoFactorCode,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
		})
		if writeImpersonationError(c, err) {
			return
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusCreated, responses.ServiceRBAC, responses.CaseSuccess,
			responses.ImpersonationStarted(started.Session, started.Target, started.AccessToken))
	}
}

// adminStopImpersonationHandler godoc
//
//	@Summary		Stop impersonating
//	@Description	Ends the session behind the impersonation token in the Authorization header, or the caller's
//	@Description	active session when called with their own token. The impersonation token stops working
//	@Description	immediately; the client switches back to the staff member's own token.
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	responses.Envelope
//	@Failure		401	{object}	responses.Envelope
//	@Failure		404	{object}	responses.Envelope	"impersonation.not_found"
//	@Security		Bearer
//	@Router			/api/v1/admin/impersonation/stop [post]
func adminStopImpersonationHandler(imp impersonationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorID, sessionID, ok := impersonationCaller(c)
		if !ok {
			return
		}

		session, err := imp.Stop(c.Request.Context(), actorID, sessionID)
		if writeImpersonationError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceRBAC, responses.CaseSuccess,
			responses.ImpersonationSession(session, false))
	}
}

// adminCurrentImpersonationHandler godoc
//
//	@Summary		Current impersonation session
//	@Description	Returns the session behind the impersonation token in the Authorization header, or the
//	@Description	caller's active session when called with their own token. data.session is null when there
//	@Description	is none.
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	responses.Envelope
//	@Failure		401	{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/impersonation/current [get]
func adminCurrentImpersonationHandler(imp impersonationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorID, sessionID, ok := impersonationCaller(c)
		if !ok {
			return
		}

		session, active, err := imp.Current(c.Request.Context(), actorID, sessionID)
		if writeImpersonationError(c, err) {
			return
		}

		data := gin.H{"active": active, "session": nil}
		if session.UUID != uuid.Nil {
			data["session"] = responses.ImpersonationSession(session, active)
		}

		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceRBAC, responses.CaseSuccess, data)
	}
}

// impersonationCaller returns the staff member behind the request and, for an impersonation
// token, its session.
func impersonationCaller(c *gin.Context) (uuid.UUID, *uuid.UUID, bool) {
	if imp, ok := middleware.CurrentImpersonation(c); ok {
		return imp.ActorID, &imp.SessionID, true
	}

	userID, ok := currentUser(c)

	return userID, nil, ok
}

func writeImpersonationError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}

	opts := responses.FailureOpts{Service: responses.ServiceRBAC}

	var status int

	switch {
	case errors.Is(err, impdomain.ErrValidation):
		status, opts.Case, opts.Code = nethttp.StatusBadRequest, responses.CaseValidation, responses.ErrorCodeValidation
		opts.Message = err.Error()
	case errors.Is(err, impdomain.ErrForbidden):
		status, opts.Case, opts.Code = nethttp.StatusForbidden, responses.CaseForbidden, "rbac.forbidden"
		opts.Message = "Insufficient permissions"
	case errors.Is(err, impdomain.ErrStepUpRequired):
		status, opts.Case, opts.Code = nethttp.StatusForbidden, responses.CaseForbidden, "impersonation.step_up_required"
		opts.Message = "Confirm with your current password and, when two-factor is enabled, a code"
	case errors.Is(err, impdomain.ErrSignInRequired):
		status, opts.Case, opts.Code = nethttp.StatusUnauthorized, responses.CaseUnauthorized, "impersonation.sign_in_required"
		opts.Message = "Refresh your access token or sign in again before impersonating"
	case errors.Is(err, impdomain.ErrIneligible):
		status, opts.Case, opts.Code = nethttp.StatusUnprocessableEntity, responses.CaseUnprocessable, "impersonation.target_ineligible"
		opts.Message = "This user cannot be impersonated"
	case errors.Is(err, impdomain.ErrAlreadyActive):
		status, opts.Case, opts.Code = nethttp.StatusConflict, responses.CaseConflict, "impersonation.already_active"
		opts.Message = "Stop the active impersonation session first"
	case errors.Is(err, impdomain.ErrNotFound):
		status, opts.Case, opts.Code = nethttp.StatusNotFound, responses.CaseNotFound, "impersonation.not_found"
		opts.Message = "No active impersonation session"
	default:
		responses.RecordError(c, err)

		status = nethttp.StatusInternalServerError
		opts.Case, opts.Code, opts.Message = responses.CaseInternalError, responses.ErrorCodeInternal, "Failed to process impersonation request"
	}

	responses.FailureFor(c, status, opts)

	return true
}
