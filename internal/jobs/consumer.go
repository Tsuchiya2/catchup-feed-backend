// Package jobs is the worker-side consumer of the jobs table (§3.3 / C-4:
// プロセス間連携は PostgreSQL のジョブテーブル経由のみ). It polls with the
// repository's SKIP LOCKED claim, dispatches to per-kind handlers, and
// records success / retry / terminal failure. Deliberately not a worker
// framework: one poll loop, one map of handlers, a fixed retry ceiling
// (§7: attempts 上限 3).
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"slices"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// Defaults, overridable through the Consumer fields.
const (
	// DefaultPollInterval is the idle sleep between empty claims. The
	// worker sits next to PostgreSQL on the Pi; a 10s poll is invisible
	// load and delivers the morning notification promptly after the radio
	// batch enqueues it (§3.3 転送検知).
	DefaultPollInterval = 10 * time.Second
	// DefaultJobTimeout bounds one handler execution. Cleanup walks a
	// directory, notify may upload an mp3 to Discord — minutes, not hours.
	DefaultJobTimeout = 5 * time.Minute
	// DefaultMaxAttempts is the §7 retry ceiling: a job is executed at
	// most this many times before failing terminally.
	DefaultMaxAttempts = 3
)

// Handler executes one job kind. Returning nil marks the job done; an
// error schedules a retry unless it is Permanent or the attempt ceiling is
// reached, in which case the job fails terminally.
type Handler interface {
	Handle(ctx context.Context, job *entity.Job) error
}

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc func(ctx context.Context, job *entity.Job) error

func (f HandlerFunc) Handle(ctx context.Context, job *entity.Job) error { return f(ctx, job) }

// permanentError marks a failure that retrying cannot fix (malformed
// payload, referenced row gone).
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent wraps err so the consumer fails the job terminally instead of
// retrying.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err (or anything it wraps) came from
// Permanent.
func IsPermanent(err error) bool {
	var p *permanentError
	return errors.As(err, &p)
}

// Consumer drains the jobs table. Zero-value fields fall back to the
// package defaults; Handlers and Jobs are required.
type Consumer struct {
	Jobs     repository.JobRepository
	Handlers map[string]Handler

	PollInterval time.Duration
	JobTimeout   time.Duration
	MaxAttempts  int
	// RetryDelay maps the attempt count (1-based, as recorded by the
	// claim) to the backoff before the next try. nil = linear minutes.
	RetryDelay func(attempts int) time.Duration
	Logger     *slog.Logger
	Now        func() time.Time // nil = time.Now
}

func (c *Consumer) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func (c *Consumer) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Consumer) pollInterval() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return DefaultPollInterval
}

func (c *Consumer) jobTimeout() time.Duration {
	if c.JobTimeout > 0 {
		return c.JobTimeout
	}
	return DefaultJobTimeout
}

func (c *Consumer) maxAttempts() int {
	if c.MaxAttempts > 0 {
		return c.MaxAttempts
	}
	return DefaultMaxAttempts
}

func (c *Consumer) retryDelay(attempts int) time.Duration {
	if c.RetryDelay != nil {
		return c.RetryDelay(attempts)
	}
	return time.Duration(attempts) * time.Minute
}

// kinds returns the job kinds this consumer claims, sorted for stable
// logs. Unregistered kinds are never claimed: they stay visibly pending in
// the table instead of being terminally failed by a binary that merely
// predates them.
func (c *Consumer) kinds() []string {
	return slices.Sorted(maps.Keys(c.Handlers))
}

// Run consumes jobs until ctx is done. It first sweeps stale 'running'
// rows of its own kinds back to pending: a running job of a kind this
// consumer handles can only be the orphan of a crashed predecessor (§4
// 持ち越し課題). Other consumers' kinds (e.g. 'transcribe' on the Mac) are
// deliberately left alone — their running rows are live, not stale. It
// always returns ctx.Err().
func (c *Consumer) Run(ctx context.Context) error {
	logger := c.logger()

	requeued, err := c.Jobs.RequeueRunning(ctx, c.kinds()...)
	if err != nil {
		// Non-fatal: the jobs themselves are still claimable next start.
		logger.Error("jobs: stale running sweep failed", slog.Any("error", err))
	} else if requeued > 0 {
		logger.Warn("jobs: requeued stale running jobs from a previous worker",
			slog.Int64("count", requeued))
	}

	logger.Info("jobs: consumer started",
		slog.Any("kinds", c.kinds()),
		slog.Duration("poll_interval", c.pollInterval()),
		slog.Int("max_attempts", c.maxAttempts()))

	for {
		claimed, err := c.consumeOne(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			logger.Error("jobs: claim failed", slog.Any("error", err))
		}
		if claimed && ctx.Err() == nil {
			continue // drain the backlog without sleeping
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.pollInterval()):
		}
	}
}

// consumeOne claims and executes at most one job. It reports whether a job
// was claimed (to keep draining without sleeping).
func (c *Consumer) consumeOne(ctx context.Context) (bool, error) {
	job, err := c.Jobs.ClaimNext(ctx, c.kinds()...)
	if err != nil || job == nil {
		return false, err
	}
	c.process(ctx, job)
	return true, nil
}

// process runs the handler and records the outcome. Handler panics are
// contained and treated as failures — one poisonous job must not kill the
// crawl worker (§8).
func (c *Consumer) process(ctx context.Context, job *entity.Job) {
	logger := c.logger().With(
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.Int("attempts", job.Attempts))
	logger.Info("jobs: job started")

	handler, ok := c.Handlers[job.Kind]
	if !ok {
		// Unreachable while kinds() drives the claim; kept as a guard for
		// future claim changes.
		c.recordFailure(ctx, job, logger, Permanent(fmt.Errorf("no handler registered for kind %q", job.Kind)))
		return
	}

	hctx, cancel := context.WithTimeout(ctx, c.jobTimeout())
	defer cancel()

	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("handler panicked: %v\n%s", r, debug.Stack())
			}
		}()
		return handler.Handle(hctx, job)
	}()

	if err == nil {
		if markErr := c.Jobs.MarkDone(ctx, job.ID); markErr != nil {
			logger.Error("jobs: mark done failed", slog.Any("error", markErr))
			return
		}
		logger.Info("jobs: job done")
		return
	}
	c.recordFailure(ctx, job, logger, err)
}

// recordFailure applies the retry policy: Permanent errors and exhausted
// attempts fail terminally, everything else is rescheduled with backoff.
func (c *Consumer) recordFailure(ctx context.Context, job *entity.Job, logger *slog.Logger, err error) {
	var retryAt *time.Time
	if !IsPermanent(err) && job.Attempts < c.maxAttempts() {
		at := c.now().Add(c.retryDelay(job.Attempts))
		retryAt = &at
	}
	if markErr := c.Jobs.MarkFailed(ctx, job.ID, err.Error(), retryAt); markErr != nil {
		logger.Error("jobs: mark failed failed", slog.Any("error", markErr), slog.Any("job_error", err))
		return
	}
	if retryAt != nil {
		logger.Warn("jobs: job failed, retry scheduled",
			slog.Time("retry_at", *retryAt), slog.Any("error", err))
		return
	}
	logger.Error("jobs: job failed terminally", slog.Any("error", err))
}
