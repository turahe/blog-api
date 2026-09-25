// Package ingestload drives the public analytics ingest routes at a fixed event rate and
// reports what the API accepted and how fast. It is a client: point it at a running API.
package ingestload

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// tick is how often the generator releases the events that are due.
	tick = 10 * time.Millisecond
	// userAgent passes the API's bot filter; events from bots are accepted but not stored.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36 ingestload"
	// droppedMetric counts events the API lost to a full queue or a failed insert.
	droppedMetric = "blog_analytics_events_dropped_total"
)

// Config tunes a run.
type Config struct {
	// BaseURL is the API origin, for example http://127.0.0.1:8080.
	BaseURL  string
	Rate     int
	Duration time.Duration
	// Workers is how many requests may be in flight; 0 picks Rate/10, at least 8.
	Workers int
	// Sessions is how many simulated visitor sessions share the events; 0 means 200.
	Sessions int
	// Spread sends X-Forwarded-For from this many addresses of 198.18.0.0/15, so the
	// per-IP ingest limit sees many clients. The API honours it only from a trusted proxy.
	Spread int
	// PathPrefix starts every generated path; empty picks /loadtest/<random>.
	PathPrefix string
	Client     *http.Client
}

// Result is what a run observed. Skipped counts events the generator could not hand to a
// worker in time: the client, not the API, was the bottleneck.
type Result struct {
	Planned, Sent, Accepted, Errors, Skipped int64
	Statuses                                 map[int]int64
	// Routes counts accepted events per ingest route (page-view, time-spent, ...).
	Routes             map[string]int64
	Elapsed            time.Duration
	P50, P95, P99, Max time.Duration
	Sessions           []uuid.UUID
	PathPrefix         string
}

// Rate is the accepted events per second.
func (r Result) Rate() float64 {
	if r.Elapsed <= 0 {
		return 0
	}

	return float64(r.Accepted) / r.Elapsed.Seconds()
}

// Check reports every way the run missed target events per second: responses other than 202,
// transport errors, skipped events, an accepted rate under minRatio of target, or a p99
// latency over maxP99 (0 disables the latency check).
func (r Result) Check(target int, minRatio float64, maxP99 time.Duration) error {
	var errs []error

	for status, n := range r.Statuses {
		if status != http.StatusAccepted {
			errs = append(errs, fmt.Errorf("%d responses with status %d", n, status))
		}
	}

	if r.Errors > 0 {
		errs = append(errs, fmt.Errorf("%d requests failed", r.Errors))
	}

	if r.Skipped > 0 {
		errs = append(errs, fmt.Errorf("%d events skipped: raise --workers", r.Skipped))
	}

	if rate := r.Rate(); rate < float64(target)*minRatio {
		errs = append(errs, fmt.Errorf("accepted %.1f events/s, want at least %.1f", rate, float64(target)*minRatio))
	}

	if maxP99 > 0 && r.P99 > maxP99 {
		errs = append(errs, fmt.Errorf("p99 latency %s over %s", r.P99, maxP99))
	}

	return errors.Join(errs...)
}

type request struct {
	route   string
	path    string
	body    []byte
	address string
}

type outcome struct {
	statuses  map[int]int64
	accepted  map[string]int64
	errors    int64
	latencies []time.Duration
}

// Run sends Rate events per second for Duration, spread over the five ingest routes like real
// traffic (page views, heartbeats, navigation, searches, and result clicks).
func Run(ctx context.Context, cfg Config) (Result, error) {
	if cfg.Rate < 1 || cfg.Duration <= 0 || cfg.BaseURL == "" {
		return Result{}, errors.New("ingestload: base URL, a positive rate, and a positive duration are required")
	}

	cfg = withDefaults(cfg)
	gen := newGenerator(cfg)
	jobs := make(chan request, cfg.Workers*2)
	outcomes := make([]outcome, cfg.Workers)

	var wg sync.WaitGroup

	for i := range cfg.Workers {
		wg.Go(func() { outcomes[i] = work(ctx, cfg, jobs) })
	}

	result := Result{Statuses: map[int]int64{}, Routes: map[string]int64{}, Sessions: gen.sessions, PathPrefix: cfg.PathPrefix}
	start := time.Now()
	result.Planned, result.Sent, result.Skipped = pace(ctx, cfg, gen, jobs, start)

	close(jobs)
	wg.Wait()

	result.Elapsed = time.Since(start)

	var latencies []time.Duration

	for _, o := range outcomes {
		for status, n := range o.statuses {
			result.Statuses[status] += n
		}

		for route, n := range o.accepted {
			result.Routes[route] += n
		}

		result.Errors += o.errors
		latencies = append(latencies, o.latencies...)
	}

	result.Accepted = result.Statuses[http.StatusAccepted]
	result.P50, result.P95, result.P99, result.Max = percentiles(latencies)

	return result, ctx.Err()
}

func withDefaults(cfg Config) Config {
	if cfg.Workers <= 0 {
		cfg.Workers = max(cfg.Rate/10, 8)
	}

	if cfg.Sessions <= 0 {
		cfg.Sessions = 200
	}

	if cfg.PathPrefix == "" {
		cfg.PathPrefix = "/loadtest/" + uuid.NewString()[:8]
	}

	if cfg.Client == nil {
		cfg.Client = &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{MaxIdleConns: cfg.Workers, MaxIdleConnsPerHost: cfg.Workers},
		}
	}

	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	return cfg
}

// pace releases the events due since start every tick until Duration has passed.
func pace(ctx context.Context, cfg Config, gen *generator, jobs chan<- request, start time.Time) (int64, int64, int64) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	var planned, sent, skipped int64

	for {
		select {
		case <-ctx.Done():
			return planned, sent, skipped
		case now := <-ticker.C:
			elapsed := min(now.Sub(start), cfg.Duration)
			due := int64(elapsed.Seconds() * float64(cfg.Rate))

			for ; planned < due; planned++ {
				select {
				case jobs <- gen.next():
					sent++
				default:
					skipped++
				}
			}

			if elapsed >= cfg.Duration {
				return planned, sent, skipped
			}
		}
	}
}

func work(ctx context.Context, cfg Config, jobs <-chan request) outcome {
	out := outcome{statuses: map[int]int64{}, accepted: map[string]int64{}}

	for job := range jobs {
		began := time.Now()

		status, err := send(ctx, cfg, job)
		if err != nil {
			out.errors++
			continue
		}

		out.latencies = append(out.latencies, time.Since(began))
		out.statuses[status]++

		if status == http.StatusAccepted {
			out.accepted[job.route]++
		}
	}

	return out
}

func send(ctx context.Context, cfg Config, job request) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+job.path, bytes.NewReader(job.body))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	if job.address != "" {
		req.Header.Set("X-Forwarded-For", job.address)
	}

	resp, err := cfg.Client.Do(req)
	if err != nil {
		return 0, err
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, resp.Body.Close()
}

func percentiles(latencies []time.Duration) (time.Duration, time.Duration, time.Duration, time.Duration) {
	if len(latencies) == 0 {
		return 0, 0, 0, 0
	}

	slices.Sort(latencies)

	at := func(q float64) time.Duration { return latencies[int(q*float64(len(latencies)-1))] }

	return at(0.50), at(0.95), at(0.99), latencies[len(latencies)-1]
}

// generator builds event requests. It is used by the pacing goroutine only.
type generator struct {
	cfg        Config
	rand       *rand.Rand
	sessions   []uuid.UUID
	lastView   map[uuid.UUID]uuid.UUID
	lastSearch map[uuid.UUID]uuid.UUID
	addresses  []string
}

func newGenerator(cfg Config) *generator {
	g := &generator{
		cfg:      cfg,
		rand:     rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x5eed)), //nolint:gosec // load shape, not security
		sessions: make([]uuid.UUID, cfg.Sessions), lastView: map[uuid.UUID]uuid.UUID{},
		lastSearch: map[uuid.UUID]uuid.UUID{},
	}

	for i := range g.sessions {
		g.sessions[i] = uuid.New()
	}

	base := netip.MustParseAddr("198.18.0.1")
	for range cfg.Spread {
		g.addresses = append(g.addresses, base.String())
		base = base.Next()
	}

	return g
}

func (g *generator) next() request {
	session := g.sessions[g.rand.IntN(len(g.sessions))]
	path := g.cfg.PathPrefix + "/posts/" + strconv.Itoa(g.rand.IntN(500))

	var (
		route string
		body  map[string]any
	)

	switch roll := g.rand.IntN(100); {
	case roll < 40:
		id := uuid.New()
		g.lastView[session] = id
		route, body = "page-view", map[string]any{"id": id, "path": path, "referrer": g.referrer()}
	case roll < 65:
		route, body = "time-spent", map[string]any{
			"viewId": g.remembered(g.lastView, session), "path": path, "focusSeconds": g.rand.IntN(300),
		}
	case roll < 85:
		route, body = "navigation", map[string]any{
			"from": g.cfg.PathPrefix + "/", "to": path, "transition": "internal",
		}
	case roll < 95:
		id := uuid.New()
		g.lastSearch[session] = id
		route, body = "search", map[string]any{
			"id": id, "query": "query " + strconv.Itoa(g.rand.IntN(200)), "resultCount": g.rand.IntN(20),
		}
	default:
		route, body = "search-click", map[string]any{
			"searchId": g.remembered(g.lastSearch, session), "position": 1 + g.rand.IntN(10),
			"resourceType": "post", "resourceId": uuid.New(),
		}
	}

	body["session_id"] = session
	raw, _ := json.Marshal(body)

	job := request{route: route, path: "/api/v1/analytics/ingest/" + route, body: raw}
	if len(g.addresses) > 0 {
		job.address = g.addresses[g.rand.IntN(len(g.addresses))]
	}

	return job
}

func (g *generator) remembered(ids map[uuid.UUID]uuid.UUID, session uuid.UUID) uuid.UUID {
	if id, ok := ids[session]; ok {
		return id
	}

	return uuid.New()
}

func (g *generator) referrer() string {
	if g.rand.IntN(4) == 0 {
		return "https://news.example/item/" + strconv.Itoa(g.rand.IntN(50))
	}

	return ""
}

// DroppedEvents reads blog_analytics_events_dropped_total from a Prometheus text endpoint.
func DroppedEvents(ctx context.Context, client *http.Client, url string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("metrics: status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if name, value, ok := strings.Cut(line, " "); ok && name == droppedMetric {
			return strconv.ParseFloat(strings.TrimSpace(value), 64)
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return 0, fmt.Errorf("metrics: %s not found", droppedMetric)
}
