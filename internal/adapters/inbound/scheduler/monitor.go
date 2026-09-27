package scheduler

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
)

// minMaxRuntime is the shortest time a run may take before Sentry marks it timed out.
const minMaxRuntime = 30 * time.Minute

// withJobHub gives the run its own Sentry hub tagged with the job, so error
// events logged with ctx are linked to the job's cron monitor.
func withJobHub(ctx context.Context, job string) context.Context {
	if sentry.CurrentHub().Client() == nil {
		return ctx
	}

	hub := sentry.CurrentHub().Clone()
	hub.Scope().SetTag("job", job)
	hub.Scope().SetContext("monitor", sentry.Context{"slug": job})

	return sentry.SetHubOnContext(ctx, hub)
}

// monitored wraps job.Run with Sentry cron check-ins on a monitor named after
// the job. It is a no-op when Sentry is not initialised.
func monitored(job Job, tick time.Duration) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		hub := sentry.GetHubFromContext(ctx)
		if hub == nil {
			hub = sentry.CurrentHub()
		}

		if hub.Client() == nil {
			return job.Run(ctx)
		}

		id := hub.CaptureCheckIn(&sentry.CheckIn{
			MonitorSlug: job.Name,
			Status:      sentry.CheckInStatusInProgress,
		}, monitorConfig(job.Every, tick))
		started := time.Now()

		err := job.Run(ctx)

		if id != nil {
			status := sentry.CheckInStatusOK
			if err != nil {
				status = sentry.CheckInStatusError
			}

			hub.CaptureCheckIn(&sentry.CheckIn{
				ID:          *id,
				MonitorSlug: job.Name,
				Status:      status,
				Duration:    time.Since(started),
			}, nil)
		}

		return err
	}
}

// monitorConfig describes a job that starts every interval, at most one tick late.
func monitorConfig(every, tick time.Duration) *sentry.MonitorConfig {
	return &sentry.MonitorConfig{
		Schedule:              intervalSchedule(every),
		CheckInMargin:         wholeMinutes(tick) + 1,
		MaxRuntime:            wholeMinutes(max(2*every, minMaxRuntime)),
		FailureIssueThreshold: 2,
		RecoveryThreshold:     1,
	}
}

func intervalSchedule(every time.Duration) sentry.MonitorSchedule {
	if every >= time.Hour && every%time.Hour == 0 {
		return sentry.IntervalSchedule(int64(every/time.Hour), sentry.MonitorScheduleUnitHour)
	}

	return sentry.IntervalSchedule(wholeMinutes(every), sentry.MonitorScheduleUnitMinute)
}

// wholeMinutes rounds d up to whole minutes, at least one.
func wholeMinutes(d time.Duration) int64 {
	return max(1, int64((d+time.Minute-1)/time.Minute))
}
