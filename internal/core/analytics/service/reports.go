package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
)

// overviewReferrers is how many traffic sources the overview lists.
const overviewReferrers = 10

// Reports answers admin dashboard queries from rollups only.
type Reports struct {
	repo  ports.ReportRepository
	tz    ports.TimezoneSource
	clock Clock
}

// NewReports returns a Reports reading rollups bucketed in the site time zone.
func NewReports(repo ports.ReportRepository, tz ports.TimezoneSource, clock Clock) *Reports {
	return &Reports{repo: repo, tz: tz, clock: clock}
}

// Header describes the windows a report covers. Comparison is the same number of periods just
// before Window; reports fill its figures only when the query asked for a comparison.
type Header struct {
	Timezone   string
	Window     domain.Window
	Comparison domain.Window
	Compare    bool
}

// Overview is the dashboard summary.
type Overview struct {
	Header

	Series     []domain.SiteRow
	Totals     domain.Totals
	Previous   *domain.Totals
	Referrers  []domain.ReferrerRow
	Dimensions []domain.DimensionRow
}

// PagesReport lists pages.
type PagesReport struct {
	Header

	Sort  string
	Pages []domain.PageRow
}

// NavigationReport lists navigation steps, entry pages, and exit pages.
type NavigationReport struct {
	Header

	Transitions []domain.TransitionRow
	Entries     []domain.PathCount
	Exits       []domain.PathCount
}

// RetentionReport lists the cohorts first seen in the window and the new versus returning
// split of consented visitors.
type RetentionReport struct {
	Header

	Cohorts  []domain.CohortRow
	Totals   domain.Totals
	Previous *domain.Totals
}

// SearchReport summarises search behaviour.
type SearchReport struct {
	Header

	Series   []domain.SiteRow
	Totals   domain.Totals
	Previous *domain.Totals
	// ClickTime sums every query, including the folded remainder.
	ClickTime   domain.QueryRow
	Queries     []domain.QueryRow
	ZeroResults []domain.QueryRow
	Positions   []domain.PositionRow
	Results     []domain.ResultRow
}

// Overview returns totals, a series per period, traffic sources, and audience dimensions.
func (r *Reports) Overview(ctx context.Context, q domain.ReportQuery) (Overview, error) {
	header, err := r.resolve(ctx, q)
	if err != nil {
		return Overview{}, err
	}

	out := Overview{Header: header}
	sel := header.Window.Selection()

	if out.Series, out.Totals, out.Previous, err = r.site(ctx, header); err != nil {
		return out, err
	}

	if out.Referrers, err = r.repo.Referrers(ctx, sel, overviewReferrers); err != nil {
		return out, err
	}

	out.Dimensions, err = r.repo.Dimensions(ctx, sel)

	return out, err
}

// Pages lists the top pages by views, average focus time, or views gained over the previous
// window.
func (r *Reports) Pages(ctx context.Context, q domain.ReportQuery) (PagesReport, error) {
	sort := q.Sort
	if sort == "" {
		sort = domain.SortViews
	}

	if sort != domain.SortViews && sort != domain.SortTime && sort != domain.SortRising {
		return PagesReport{}, fmt.Errorf("%w: sort must be views, time, or rising", domain.ErrValidation)
	}

	header, err := r.resolve(ctx, q)
	if err != nil {
		return PagesReport{}, err
	}

	pages, err := r.repo.Pages(ctx, header.Window.Selection(), header.Comparison.Selection(), sort, limitOf(q))

	return PagesReport{Header: header, Sort: sort, Pages: pages}, err
}

// Navigation lists the most common steps between pages and the top entry and exit pages.
func (r *Reports) Navigation(ctx context.Context, q domain.ReportQuery) (NavigationReport, error) {
	header, err := r.resolve(ctx, q)
	if err != nil {
		return NavigationReport{}, err
	}

	out := NavigationReport{Header: header}
	sel, limit := header.Window.Selection(), limitOf(q)

	if out.Transitions, err = r.repo.Transitions(ctx, sel, limit); err != nil {
		return out, err
	}

	if out.Entries, err = r.repo.EntryPages(ctx, sel, limit); err != nil {
		return out, err
	}

	out.Exits, err = r.repo.ExitPages(ctx, sel, limit)

	return out, err
}

// Retention lists the daily cohorts of the window's days and the site totals for the new versus
// returning split.
func (r *Reports) Retention(ctx context.Context, q domain.ReportQuery) (RetentionReport, error) {
	header, err := r.resolve(ctx, q)
	if err != nil {
		return RetentionReport{}, err
	}

	out := RetentionReport{Header: header}

	if _, out.Totals, out.Previous, err = r.site(ctx, header); err != nil {
		return out, err
	}

	out.Cohorts, err = r.repo.Cohorts(ctx, header.Window.FirstDay(), header.Window.LastDay())

	return out, err
}

// Search returns search totals and series, top and zero-result queries, clicks per position,
// and the most clicked results.
func (r *Reports) Search(ctx context.Context, q domain.ReportQuery) (SearchReport, error) {
	header, err := r.resolve(ctx, q)
	if err != nil {
		return SearchReport{}, err
	}

	out := SearchReport{Header: header}
	sel, limit := header.Window.Selection(), limitOf(q)

	if out.Series, out.Totals, out.Previous, err = r.site(ctx, header); err != nil {
		return out, err
	}

	if out.ClickTime, err = r.repo.QueryTotals(ctx, sel); err != nil {
		return out, err
	}

	if out.Queries, err = r.repo.Queries(ctx, sel, false, limit); err != nil {
		return out, err
	}

	if out.ZeroResults, err = r.repo.Queries(ctx, sel, true, limit); err != nil {
		return out, err
	}

	if out.Positions, err = r.repo.Positions(ctx, sel, domain.MaxReportLimit); err != nil {
		return out, err
	}

	out.Results, err = r.repo.ClickedResults(ctx, sel, limit)

	return out, err
}

// site returns the window's series (every period, zero when it has no rollup row), its totals,
// and the previous window's totals when comparing.
func (r *Reports) site(ctx context.Context, h Header) ([]domain.SiteRow, domain.Totals, *domain.Totals, error) {
	rows, err := r.repo.SiteRows(ctx, h.Window.Selection())
	if err != nil {
		return nil, domain.Totals{}, nil, err
	}

	series, totals := fill(h.Window, rows)

	if !h.Compare {
		return series, totals, nil, nil
	}

	prevRows, err := r.repo.SiteRows(ctx, h.Comparison.Selection())
	if err != nil {
		return nil, domain.Totals{}, nil, err
	}

	_, previous := fill(h.Comparison, prevRows)

	return series, totals, &previous, nil
}

func fill(w domain.Window, rows []domain.SiteRow) ([]domain.SiteRow, domain.Totals) {
	byPeriod := make(map[string]domain.SiteRow, len(rows))
	for _, row := range rows {
		byPeriod[row.Period] = row
	}

	series := make([]domain.SiteRow, len(w.Periods))
	totals := domain.Totals{Periods: len(w.Periods)}

	for i, p := range w.Periods {
		row := byPeriod[p.Day()]
		row.Period = p.Day()
		series[i] = row
		totals.Add(row)
	}

	return series, totals
}

// resolve validates the query and turns it into the current and previous windows.
func (r *Reports) resolve(ctx context.Context, q domain.ReportQuery) (Header, error) {
	return resolveHeader(ctx, r.tz, r.clock, q)
}

// resolveHeader validates q and turns it into whole periods of the site time zone.
func resolveHeader(ctx context.Context, tz ports.TimezoneSource, clock Clock, q domain.ReportQuery) (Header, error) {
	if q.Limit < 0 || q.Limit > domain.MaxReportLimit {
		return Header{}, fmt.Errorf("%w: limit must be between 1 and %d", domain.ErrValidation, domain.MaxReportLimit)
	}

	if q.Grain != "" && !domain.ValidGrain(q.Grain) {
		return Header{}, fmt.Errorf("%w: grain must be day, week, or month", domain.ErrValidation)
	}

	name, err := tz.Timezone(ctx)
	if err != nil {
		return Header{}, err
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return Header{}, fmt.Errorf("site time zone %q: %w", name, err)
	}

	from, to := reportDates(q, clock.Now().In(loc), loc)
	if from.After(to) {
		return Header{}, fmt.Errorf("%w: from is after to", domain.ErrValidation)
	}

	days := int(math.Round(to.Sub(from).Hours()/24)) + 1
	if days > domain.MaxReportDays {
		return Header{}, fmt.Errorf("%w: the range is longer than %d days", domain.ErrValidation, domain.MaxReportDays)
	}

	grain := q.Grain
	if grain == "" {
		grain = domain.AutoGrain(days)
	}

	current := domain.Window{Grain: grain, Periods: domain.PeriodsBetween(grain, from, to, loc)}
	previous := domain.Window{Grain: grain, Periods: make([]domain.Period, len(current.Periods))}

	p := current.Periods[0]
	for i := len(previous.Periods) - 1; i >= 0; i-- {
		p = domain.PeriodOf(grain, p.Start.AddDate(0, 0, -1), loc)
		previous.Periods[i] = p
	}

	return Header{Timezone: name, Window: current, Comparison: previous, Compare: q.Compare}, nil
}

// reportDates returns the query's first and last local days, defaulting to the
// DefaultReportDays days through today.
func reportDates(q domain.ReportQuery, today time.Time, loc *time.Location) (time.Time, time.Time) {
	to := localDate(today, loc)
	if !q.To.IsZero() {
		to = localDate(q.To, loc)
	}

	from := to.AddDate(0, 0, -(domain.DefaultReportDays - 1))
	if !q.From.IsZero() {
		from = localDate(q.From, loc)
	}

	return from, to
}

func limitOf(q domain.ReportQuery) int {
	if q.Limit == 0 {
		return domain.DefaultReportLimit
	}

	return q.Limit
}
