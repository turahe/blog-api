package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/platform/logging"
	sentryplatform "github.com/turahe/blog-api/internal/platform/sentry"
)

type captureTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (c *captureTransport) Flush(time.Duration) bool              { return true }
func (c *captureTransport) FlushWithContext(context.Context) bool { return true }
func (c *captureTransport) Configure(sentry.ClientOptions)        {}
func (c *captureTransport) Close()                                {}

func (c *captureTransport) SendEvent(event *sentry.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, event)
}

func (c *captureTransport) split() (checkIns, errs []*sentry.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, e := range c.events {
		if e.Type == "check_in" {
			checkIns = append(checkIns, e)
		} else {
			errs = append(errs, e)
		}
	}

	return checkIns, errs
}

func bindSentry(t *testing.T) *captureTransport {
	t.Helper()

	transport := &captureTransport{}
	require.NoError(t, sentry.Init(sentry.ClientOptions{
		Dsn:       "https://public@example.com/1",
		Transport: transport,
	}))
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	return transport
}

//nolint:paralleltest // binds the global Sentry hub
func TestRunSendsCheckInsForAJobThatRan(t *testing.T) {
	transport := bindSentry(t)
	logger := logging.NewTo(io.Discard, "production", sentryplatform.NewHandler())
	s := New(&fakeStore{}, time.Minute, logger,
		Job{Name: "audit-prune", Every: time.Hour, Run: func(context.Context) error { return nil }},
		Job{Name: "newsletter-release", Every: time.Minute, Run: func(context.Context) error { return errors.New("smtp down") }},
	)

	_, err := s.RunNow(t.Context(), "audit-prune")
	require.NoError(t, err)
	_, err = s.RunNow(t.Context(), "newsletter-release")
	require.Error(t, err)

	checkIns, errs := transport.split()
	require.Len(t, checkIns, 4)

	start, done := checkIns[0], checkIns[1]
	require.Equal(t, "audit-prune", start.CheckIn.MonitorSlug)
	require.Equal(t, sentry.CheckInStatusInProgress, start.CheckIn.Status)
	require.Equal(t, sentry.IntervalSchedule(1, sentry.MonitorScheduleUnitHour), start.MonitorConfig.Schedule)
	require.Equal(t, int64(2), start.MonitorConfig.CheckInMargin)
	require.Equal(t, int64(120), start.MonitorConfig.MaxRuntime)
	require.Equal(t, start.CheckIn.ID, done.CheckIn.ID)
	require.Equal(t, sentry.CheckInStatusOK, done.CheckIn.Status)
	require.Nil(t, done.MonitorConfig)

	require.Equal(t, sentry.IntervalSchedule(1, sentry.MonitorScheduleUnitMinute), checkIns[2].MonitorConfig.Schedule)
	require.Equal(t, int64(30), checkIns[2].MonitorConfig.MaxRuntime)
	require.Equal(t, sentry.CheckInStatusError, checkIns[3].CheckIn.Status)

	require.Len(t, errs, 1, "the failure is also reported as an error event")
	require.Equal(t, "newsletter-release", errs[0].Tags["job"])
	require.Equal(t, "newsletter-release", errs[0].Contexts["monitor"]["slug"])
}

//nolint:paralleltest // binds the global Sentry hub
func TestRunSkipsCheckInsWhenAnotherReplicaHoldsTheJob(t *testing.T) {
	transport := bindSentry(t)
	s := New(&fakeStore{busy: true}, time.Minute, slog.New(slog.DiscardHandler),
		Job{Name: "audit-prune", Every: time.Hour, Run: func(context.Context) error { return nil }},
	)

	ran, err := s.RunNow(t.Context(), "audit-prune")
	require.NoError(t, err)
	require.False(t, ran)

	checkIns, _ := transport.split()
	require.Empty(t, checkIns)
}

func TestIntervalSchedule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		every time.Duration
		want  sentry.MonitorSchedule
	}{
		{time.Minute, sentry.IntervalSchedule(1, sentry.MonitorScheduleUnitMinute)},
		{15 * time.Minute, sentry.IntervalSchedule(15, sentry.MonitorScheduleUnitMinute)},
		{90 * time.Minute, sentry.IntervalSchedule(90, sentry.MonitorScheduleUnitMinute)},
		{time.Hour, sentry.IntervalSchedule(1, sentry.MonitorScheduleUnitHour)},
		{24 * time.Hour, sentry.IntervalSchedule(24, sentry.MonitorScheduleUnitHour)},
		{time.Millisecond, sentry.IntervalSchedule(1, sentry.MonitorScheduleUnitMinute)},
	} {
		require.Equal(t, tc.want, intervalSchedule(tc.every), tc.every)
	}
}
