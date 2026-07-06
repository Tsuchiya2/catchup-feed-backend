package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/jobs"
)

// fakeJobQueue is an in-memory repository.JobRepository.
type fakeJobQueue struct {
	mu       sync.Mutex
	jobs     []*entity.Job
	nextID   int64
	claimErr error
}

func (q *fakeJobQueue) add(kind string, status string, attempts int, payload string) *entity.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextID++
	job := &entity.Job{
		ID:       q.nextID,
		Kind:     kind,
		Status:   status,
		Attempts: attempts,
		Payload:  json.RawMessage(payload),
		RunAfter: time.Now().Add(-time.Second),
	}
	q.jobs = append(q.jobs, job)
	return job
}

// get returns a copy of the job so test assertions never race the
// consumer goroutine mutating the shared row.
func (q *fakeJobQueue) get(id int64) *entity.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, job := range q.jobs {
		if job.ID == id {
			copied := *job
			return &copied
		}
	}
	return nil
}

func (q *fakeJobQueue) Enqueue(_ context.Context, kind string, payload json.RawMessage, _ time.Time) (int64, error) {
	job := q.add(kind, entity.JobStatusPending, 0, string(payload))
	return job.ID, nil
}

func (q *fakeJobQueue) ClaimNext(_ context.Context, kinds ...string) (*entity.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.claimErr != nil {
		return nil, q.claimErr
	}
	for _, job := range q.jobs {
		if job.Status != entity.JobStatusPending || time.Now().Before(job.RunAfter) {
			continue
		}
		if len(kinds) > 0 && !slices.Contains(kinds, job.Kind) {
			continue
		}
		job.Status = entity.JobStatusRunning
		job.Attempts++
		copied := *job
		return &copied, nil
	}
	return nil, nil
}

func (q *fakeJobQueue) MarkDone(_ context.Context, id int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, job := range q.jobs {
		if job.ID == id {
			job.Status = entity.JobStatusDone
			return nil
		}
	}
	return errors.New("not found")
}

func (q *fakeJobQueue) MarkFailed(_ context.Context, id int64, lastError string, retryAt *time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, job := range q.jobs {
		if job.ID != id {
			continue
		}
		job.LastError = &lastError
		if retryAt != nil {
			job.Status = entity.JobStatusPending
			job.RunAfter = *retryAt
		} else {
			job.Status = entity.JobStatusFailed
		}
		return nil
	}
	return errors.New("not found")
}

func (q *fakeJobQueue) RequeueRunning(_ context.Context, kinds ...string) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var n int64
	for _, job := range q.jobs {
		if job.Status == entity.JobStatusRunning && slices.Contains(kinds, job.Kind) {
			job.Status = entity.JobStatusPending
			n++
		}
	}
	return n, nil
}

// runUntil runs the consumer until check(queue) is true or the timeout hits.
func runUntil(t *testing.T, consumer *jobs.Consumer, queue *fakeJobQueue, check func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = consumer.Run(ctx)
	}()

	deadline := time.After(5 * time.Second)
	for {
		if check() {
			cancel()
			<-done
			return
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("condition not reached within timeout")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func newTestConsumer(queue *fakeJobQueue, handlers map[string]jobs.Handler) *jobs.Consumer {
	return &jobs.Consumer{
		Jobs:         queue,
		Handlers:     handlers,
		PollInterval: 5 * time.Millisecond,
		RetryDelay:   func(int) time.Duration { return 0 },
		Logger:       slog.New(slog.DiscardHandler),
	}
}

func TestConsumer_Run(t *testing.T) {
	t.Run("no handlers: Run refuses instead of claiming every kind", func(t *testing.T) {
		// An empty Handlers map would make ClaimNext run unrestricted and
		// terminally fail other consumers' pending jobs (e.g. 'transcribe'
		// waiting for the Mac worker) with "no handler registered".
		queue := &fakeJobQueue{}
		foreign := queue.add("transcribe", entity.JobStatusPending, 0, `{}`)

		for name, handlers := range map[string]map[string]jobs.Handler{
			"nil map":   nil,
			"empty map": {},
		} {
			consumer := newTestConsumer(queue, handlers)
			err := consumer.Run(context.Background())
			require.Error(t, err, name)
			assert.Contains(t, err.Error(), "no registered handlers", name)
		}

		// The foreign pending job was never touched.
		got := queue.get(foreign.ID)
		assert.Equal(t, entity.JobStatusPending, got.Status)
		assert.Equal(t, 0, got.Attempts)
	})

	t.Run("success marks the job done", func(t *testing.T) {
		queue := &fakeJobQueue{}
		job := queue.add("ok", entity.JobStatusPending, 0, `{}`)

		var handled []int64
		var mu sync.Mutex
		consumer := newTestConsumer(queue, map[string]jobs.Handler{
			"ok": jobs.HandlerFunc(func(_ context.Context, j *entity.Job) error {
				mu.Lock()
				handled = append(handled, j.ID)
				mu.Unlock()
				return nil
			}),
		})
		runUntil(t, consumer, queue, func() bool { return queue.get(job.ID).Status == entity.JobStatusDone })
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, []int64{job.ID}, handled)
	})

	t.Run("transient failure retries until done (§7)", func(t *testing.T) {
		queue := &fakeJobQueue{}
		job := queue.add("flaky", entity.JobStatusPending, 0, `{}`)

		var calls int
		var mu sync.Mutex
		consumer := newTestConsumer(queue, map[string]jobs.Handler{
			"flaky": jobs.HandlerFunc(func(_ context.Context, _ *entity.Job) error {
				mu.Lock()
				defer mu.Unlock()
				calls++
				if calls < 3 {
					return errors.New("transient")
				}
				return nil
			}),
		})
		runUntil(t, consumer, queue, func() bool { return queue.get(job.ID).Status == entity.JobStatusDone })
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, 3, calls)
		assert.Equal(t, 3, queue.get(job.ID).Attempts)
	})

	t.Run("failures beyond max attempts are terminal (attempts 上限 3)", func(t *testing.T) {
		queue := &fakeJobQueue{}
		job := queue.add("broken", entity.JobStatusPending, 0, `{}`)

		var calls int
		var mu sync.Mutex
		consumer := newTestConsumer(queue, map[string]jobs.Handler{
			"broken": jobs.HandlerFunc(func(_ context.Context, _ *entity.Job) error {
				mu.Lock()
				calls++
				mu.Unlock()
				return errors.New("always fails")
			}),
		})
		runUntil(t, consumer, queue, func() bool { return queue.get(job.ID).Status == entity.JobStatusFailed })
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, jobs.DefaultMaxAttempts, calls)
		require.NotNil(t, queue.get(job.ID).LastError)
		assert.Contains(t, *queue.get(job.ID).LastError, "always fails")
	})

	t.Run("permanent error fails terminally on the first attempt", func(t *testing.T) {
		queue := &fakeJobQueue{}
		job := queue.add("poison", entity.JobStatusPending, 0, `{}`)

		var calls int
		var mu sync.Mutex
		consumer := newTestConsumer(queue, map[string]jobs.Handler{
			"poison": jobs.HandlerFunc(func(_ context.Context, _ *entity.Job) error {
				mu.Lock()
				calls++
				mu.Unlock()
				return jobs.Permanent(errors.New("bad payload"))
			}),
		})
		runUntil(t, consumer, queue, func() bool { return queue.get(job.ID).Status == entity.JobStatusFailed })
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, 1, calls)
	})

	t.Run("handler panic is contained and counts as a failure", func(t *testing.T) {
		queue := &fakeJobQueue{}
		job := queue.add("panicky", entity.JobStatusPending, jobs.DefaultMaxAttempts-1, `{}`)

		consumer := newTestConsumer(queue, map[string]jobs.Handler{
			"panicky": jobs.HandlerFunc(func(_ context.Context, _ *entity.Job) error {
				panic("boom")
			}),
		})
		runUntil(t, consumer, queue, func() bool { return queue.get(job.ID).Status == entity.JobStatusFailed })
		require.NotNil(t, queue.get(job.ID).LastError)
		assert.Contains(t, *queue.get(job.ID).LastError, "handler panicked")
	})

	t.Run("stale running jobs are requeued at startup and executed", func(t *testing.T) {
		queue := &fakeJobQueue{}
		orphan := queue.add("ok", entity.JobStatusRunning, 1, `{}`) // crashed predecessor

		consumer := newTestConsumer(queue, map[string]jobs.Handler{
			"ok": jobs.HandlerFunc(func(_ context.Context, _ *entity.Job) error { return nil }),
		})
		runUntil(t, consumer, queue, func() bool { return queue.get(orphan.ID).Status == entity.JobStatusDone })
		assert.Equal(t, 2, queue.get(orphan.ID).Attempts, "the crashed claim's attempt must stay counted")
	})

	t.Run("startup sweep only touches this consumer's kinds", func(t *testing.T) {
		queue := &fakeJobQueue{}
		// A transcribe job mid-execution on another consumer (Mac worker)
		// must survive this consumer's startup sweep untouched.
		foreign := queue.add(entity.JobKindTranscribe, entity.JobStatusRunning, 1, `{}`)
		orphan := queue.add("ok", entity.JobStatusRunning, 1, `{}`)

		consumer := newTestConsumer(queue, map[string]jobs.Handler{
			"ok": jobs.HandlerFunc(func(_ context.Context, _ *entity.Job) error { return nil }),
		})
		runUntil(t, consumer, queue, func() bool { return queue.get(orphan.ID).Status == entity.JobStatusDone })
		got := queue.get(foreign.ID)
		assert.Equal(t, entity.JobStatusRunning, got.Status,
			"another consumer's running job must not be requeued")
		assert.Equal(t, 1, got.Attempts,
			"another consumer's running job must not gain attempts")
	})

	t.Run("unregistered kinds are never claimed", func(t *testing.T) {
		queue := &fakeJobQueue{}
		future := queue.add("future_kind", entity.JobStatusPending, 0, `{}`)
		known := queue.add("ok", entity.JobStatusPending, 0, `{}`)

		consumer := newTestConsumer(queue, map[string]jobs.Handler{
			"ok": jobs.HandlerFunc(func(_ context.Context, _ *entity.Job) error { return nil }),
		})
		runUntil(t, consumer, queue, func() bool { return queue.get(known.ID).Status == entity.JobStatusDone })
		assert.Equal(t, entity.JobStatusPending, queue.get(future.ID).Status,
			"a kind this binary does not know must stay pending, visible in the table")
	})

	t.Run("run returns when the context is canceled", func(t *testing.T) {
		queue := &fakeJobQueue{}
		// At least one handler: an empty consumer is rejected before the
		// poll loop and would never observe the cancellation.
		consumer := newTestConsumer(queue, map[string]jobs.Handler{
			"noop": jobs.HandlerFunc(func(context.Context, *entity.Job) error { return nil }),
		})

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- consumer.Run(ctx) }()
		cancel()
		select {
		case err := <-done:
			assert.ErrorIs(t, err, context.Canceled)
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not stop on cancellation")
		}
	})
}

func TestPermanent(t *testing.T) {
	base := errors.New("x")
	assert.True(t, jobs.IsPermanent(jobs.Permanent(base)))
	assert.True(t, jobs.IsPermanent(errors.Join(errors.New("other"), jobs.Permanent(base))))
	assert.False(t, jobs.IsPermanent(base))
	assert.NoError(t, jobs.Permanent(nil))
	assert.ErrorIs(t, jobs.Permanent(base), base)
}
