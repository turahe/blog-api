// Package captcha verifies human-check tokens with an external provider.
package captcha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// TurnstileEndpoint is Cloudflare's siteverify URL.
const TurnstileEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

const (
	turnstileTimeout  = 5 * time.Second
	maxResponseBytes  = 64 << 10
	maxTurnstileToken = 2048
)

// providerFaults are Turnstile error codes caused by our configuration or Cloudflare,
// not by the visitor's token.
var providerFaults = []string{"missing-input-secret", "invalid-input-secret", "bad-request", "internal-error"}

// Turnstile verifies Cloudflare Turnstile tokens.
type Turnstile struct {
	secret   string
	endpoint string
	client   *http.Client
}

// NewTurnstile returns a verifier for secret. An empty endpoint uses TurnstileEndpoint
// and a nil client uses one with a 5s timeout.
func NewTurnstile(secret, endpoint string, client *http.Client) *Turnstile {
	if endpoint == "" {
		endpoint = TurnstileEndpoint
	}

	if client == nil {
		client = &http.Client{Timeout: turnstileTimeout}
	}

	return &Turnstile{secret: secret, endpoint: endpoint, client: client}
}

type siteverifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// Verify reports whether token is a valid, unused Turnstile token for remoteIP. A
// non-nil error means the check could not be completed, not that the token is bad.
func (t *Turnstile) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > maxTurnstileToken {
		return false, nil
	}

	form := url.Values{"secret": {t.secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, fmt.Errorf("turnstile request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("turnstile siteverify: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("turnstile siteverify: status %d", resp.StatusCode)
	}

	var body siteverifyResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&body); err != nil {
		return false, fmt.Errorf("turnstile siteverify: decode: %w", err)
	}

	if body.Success {
		return true, nil
	}

	for _, code := range body.ErrorCodes {
		if slices.Contains(providerFaults, code) {
			return false, fmt.Errorf("turnstile siteverify: %s", code)
		}
	}

	return false, nil
}
