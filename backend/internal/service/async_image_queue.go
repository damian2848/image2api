package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	asyncImageQueueStream       = "queue:v1:image:stream"
	asyncImageQueueGroup        = "queue:v1:image:workers"
	asyncImageQueueScheduled    = "queue:v1:image:scheduled"
	asyncImageQueueJobKeyPrefix = "queue:v1:image:job:"

	asyncImageQueueReadBlock        = time.Second
	asyncImageQueuePromoteEvery     = 250 * time.Millisecond
	asyncImageQueueReclaimEvery     = 30 * time.Second
	asyncImageQueueHeartbeatEvery   = time.Minute
	asyncImageQueueClaimIdle        = 8*time.Minute + 30*time.Second
	asyncImageQueueExecutionTimeout = 9 * time.Minute
	asyncImageQueueMaxAttempts      = 3
	asyncImageQueuePromoteBatch     = 100
	asyncImageQueueMaxDepth         = 10_000
	asyncImageQueueMaxPayloadBytes  = 64 << 20
)

var ErrAsyncImageQueueFull = errors.New("async image queue is full")

// AsyncImageJob is the durable description of one accepted asynchronous image
// request. The event row is the source of truth for billing and terminal state;
// Redis owns delivery, retries, and crash recovery.
type AsyncImageJob struct {
	Version    int            `json:"version"`
	EventID    string         `json:"event_id"`
	UserID     string         `json:"user_id,omitempty"`
	TokenType  string         `json:"token_type,omitempty"`
	Request    V1ImageRequest `json:"request"`
	Attempts   int            `json:"attempts"`
	Exhausted  bool           `json:"exhausted,omitempty"`
	LastError  string         `json:"last_error,omitempty"`
	EnqueuedAt time.Time      `json:"enqueued_at"`
}

type asyncImageJobOutcome struct {
	err             error
	retry           bool
	retryAfter      time.Duration
	preserveAttempt bool
}

type asyncImageJobHandler func(context.Context, AsyncImageJob) asyncImageJobOutcome
type asyncImageJobExhausted func(context.Context, AsyncImageJob, error) error

// AsyncImageQueue is a Redis Streams backed, at-least-once worker queue. Stream
// entries contain only an event ID; the full payload lives under a stable Redis
// key so delayed retries can move between the stream and sorted set atomically.
type AsyncImageQueue struct {
	redis          *redis.Client
	workers        int
	handle         asyncImageJobHandler
	exhausted      asyncImageJobExhausted
	consumerID     string
	readBlock      time.Duration
	promoteEvery   time.Duration
	reclaimEvery   time.Duration
	heartbeatEvery time.Duration
	claimIdle      time.Duration
	maxAttempts    int
	maxDepth       int
	maxPayload     int
	slots          chan struct{}
}

func newAsyncImageQueue(rdb *redis.Client, workers int, handle asyncImageJobHandler, exhausted asyncImageJobExhausted) *AsyncImageQueue {
	if workers < 1 {
		workers = 1
	}
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		host = "worker"
	}
	return &AsyncImageQueue{
		redis:          rdb,
		workers:        workers,
		handle:         handle,
		exhausted:      exhausted,
		consumerID:     fmt.Sprintf("%s-%d-%s", host, os.Getpid(), randomUpper(6)),
		readBlock:      asyncImageQueueReadBlock,
		promoteEvery:   asyncImageQueuePromoteEvery,
		reclaimEvery:   asyncImageQueueReclaimEvery,
		heartbeatEvery: asyncImageQueueHeartbeatEvery,
		claimIdle:      asyncImageQueueClaimIdle,
		maxAttempts:    asyncImageQueueMaxAttempts,
		maxDepth:       asyncImageQueueMaxDepth,
		maxPayload:     asyncImageQueueMaxPayloadBytes,
		slots:          make(chan struct{}, workers),
	}
}

// EnableAsyncImageQueue wires the durable queue to the V1 service. Bootstrap
// starts the returned queue with the application lifetime context.
func (s *V1Service) EnableAsyncImageQueue(rdb *redis.Client, workers int) *AsyncImageQueue {
	queue := newAsyncImageQueue(rdb, workers, s.handleQueuedImageJob, s.failQueuedImageJob)
	s.asyncImages = queue
	return queue
}

func (s *V1Service) handleQueuedImageJob(ctx context.Context, job AsyncImageJob) asyncImageJobOutcome {
	event, err := s.events.GetByID(ctx, job.EventID)
	if err != nil {
		return asyncImageJobOutcome{err: err, retry: true}
	}
	if event == nil || event.Status == "success" || event.Status == "failed" {
		return asyncImageJobOutcome{}
	}
	if err := s.events.SetQueueState(ctx, job.EventID, "processing"); err != nil {
		return asyncImageJobOutcome{err: err, retry: true}
	}

	principal := &APIPrincipal{TokenType: job.TokenType}
	if strings.TrimSpace(event.UserID) != "" {
		user, loadErr := s.users.GetByID(ctx, event.UserID)
		if loadErr != nil {
			_ = s.events.SetQueueState(context.WithoutCancel(ctx), job.EventID, "queued")
			return asyncImageJobOutcome{err: loadErr, retry: true}
		}
		principal.User = user
	}
	request := job.Request
	request.existingEventID = job.EventID
	// existingEventID bypasses charging; charge=true preserves post-success
	// accounting such as the first-generation invite reward.
	_, execErr := s.prepareImageExecution(ctx, principal, request, "v1_async", true)
	if execErr == nil {
		return asyncImageJobOutcome{}
	}

	after, loadErr := s.events.GetByID(context.WithoutCancel(ctx), job.EventID)
	if loadErr != nil {
		return asyncImageJobOutcome{err: fmt.Errorf("%v; reload event: %w", execErr, loadErr), retry: true}
	}
	if after == nil || after.Status == "success" || after.Status == "failed" {
		return asyncImageJobOutcome{err: execErr}
	}
	_ = s.events.SetQueueState(context.WithoutCancel(ctx), job.EventID, "queued")
	delay := time.Duration(0)
	preserveAttempt := false
	if errors.Is(execErr, ErrUserConcurrencyFull) {
		delay = time.Second
		preserveAttempt = true
	}
	return asyncImageJobOutcome{err: execErr, retry: true, retryAfter: delay, preserveAttempt: preserveAttempt}
}

func (s *V1Service) failQueuedImageJob(ctx context.Context, job AsyncImageJob, cause error) error {
	event, err := s.events.GetByID(ctx, job.EventID)
	if err != nil {
		return err
	}
	if event == nil || event.Status == "success" || event.Status == "failed" {
		return nil
	}
	principal := &APIPrincipal{TokenType: job.TokenType}
	if strings.TrimSpace(event.UserID) != "" {
		principal.User, _ = s.users.GetByID(ctx, event.UserID)
	}
	if principal.User != nil {
		if err := s.refundIfNeeded(ctx, principal, event.ID, event.Cost); err != nil {
			return err
		}
	}
	message := "async image queue exhausted"
	if cause != nil {
		message += ": " + cause.Error()
	}
	return s.events.UpdateStatus(ctx, event.ID, "failed", message, 0)
}

var enqueueAsyncImageScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then
  return 0
end
if redis.call('XLEN', KEYS[2]) + redis.call('ZCARD', KEYS[3]) >= tonumber(ARGV[3]) then
  return -1
end
redis.call('SET', KEYS[1], ARGV[1])
redis.call('XADD', KEYS[2], '*', 'event_id', ARGV[2])
return 1
`)

var finishAsyncImageScript = redis.NewScript(`
redis.call('DEL', KEYS[1])
redis.call('ZREM', KEYS[2], ARGV[3])
redis.call('XACK', KEYS[3], ARGV[1], ARGV[2])
redis.call('XDEL', KEYS[3], ARGV[2])
return 1
`)

var retryAsyncImageScript = redis.NewScript(`
redis.call('SET', KEYS[1], ARGV[1])
redis.call('ZADD', KEYS[2], ARGV[2], ARGV[3])
redis.call('XACK', KEYS[3], ARGV[4], ARGV[5])
redis.call('XDEL', KEYS[3], ARGV[5])
return 1
`)

var promoteAsyncImagesScript = redis.NewScript(`
local ids = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
for _, id in ipairs(ids) do
  if redis.call('EXISTS', ARGV[3] .. id) == 1 then
    redis.call('XADD', KEYS[2], '*', 'event_id', id)
  end
  redis.call('ZREM', KEYS[1], id)
end
return #ids
`)

func (q *AsyncImageQueue) jobKey(eventID string) string {
	return asyncImageQueueJobKeyPrefix + strings.TrimSpace(eventID)
}

// Enqueue durably stores the payload and publishes its event ID in one Redis
// transaction. Repeating the same event ID is idempotent.
func (q *AsyncImageQueue) Enqueue(ctx context.Context, job AsyncImageJob) error {
	if q == nil || q.redis == nil {
		return errors.New("async image queue is not configured")
	}
	job.EventID = strings.TrimSpace(job.EventID)
	if job.EventID == "" {
		return errors.New("async image job event id is required")
	}
	if job.Version == 0 {
		job.Version = 1
	}
	if job.EnqueuedAt.IsZero() {
		job.EnqueuedAt = time.Now()
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("encode async image job: %w", err)
	}
	if q.maxPayload > 0 && len(payload) > q.maxPayload {
		return fmt.Errorf("async image job payload exceeds %d bytes", q.maxPayload)
	}
	result, err := enqueueAsyncImageScript.Run(ctx, q.redis,
		[]string{q.jobKey(job.EventID), asyncImageQueueStream, asyncImageQueueScheduled},
		payload, job.EventID, q.maxDepth).Int()
	if err != nil {
		return fmt.Errorf("enqueue async image job: %w", err)
	}
	if result < 0 {
		return ErrAsyncImageQueueFull
	}
	return nil
}

// Start creates the consumer group before accepting work and launches the
// independent workers plus delayed-retry and stale-claim loops.
func (q *AsyncImageQueue) Start(ctx context.Context) error {
	if q == nil || q.redis == nil || q.handle == nil {
		return errors.New("async image queue is not configured")
	}
	if err := q.redis.XGroupCreateMkStream(ctx, asyncImageQueueStream, asyncImageQueueGroup, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create async image consumer group: %w", err)
	}
	for i := 0; i < q.workers; i++ {
		go q.runWorker(ctx, fmt.Sprintf("%s-%02d", q.consumerID, i))
	}
	go q.runPromoter(ctx)
	go q.runReclaimer(ctx)
	log.Printf("async image queue started: workers=%d consumer=%s", q.workers, q.consumerID)
	return nil
}

func (q *AsyncImageQueue) runWorker(ctx context.Context, consumer string) {
	for ctx.Err() == nil {
		streams, err := q.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    asyncImageQueueGroup,
			Consumer: consumer,
			Streams:  []string{asyncImageQueueStream, ">"},
			Count:    1,
			Block:    q.readBlock,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) || ctx.Err() != nil {
				continue
			}
			log.Printf("async image queue read failed: consumer=%s err=%v", consumer, err)
			continue
		}
		for _, stream := range streams {
			for _, message := range stream.Messages {
				q.processMessage(message, consumer)
			}
		}
	}
}

func (q *AsyncImageQueue) processMessage(message redis.XMessage, consumer string) {
	q.slots <- struct{}{}
	defer func() { <-q.slots }()
	stopHeartbeat := make(chan struct{})
	go q.heartbeatPending(message.ID, consumer, stopHeartbeat)
	defer close(stopHeartbeat)
	eventID := strings.TrimSpace(fmt.Sprint(message.Values["event_id"]))
	if eventID == "" {
		q.finishMessage(message.ID, "")
		return
	}
	bookkeepingCtx, cancelBookkeeping := context.WithTimeout(context.Background(), 10*time.Second)
	payload, err := q.redis.Get(bookkeepingCtx, q.jobKey(eventID)).Bytes()
	cancelBookkeeping()
	if errors.Is(err, redis.Nil) {
		if !q.handleUnrecoverableJob(eventID, errors.New("async image job payload is missing")) {
			return
		}
		q.finishMessage(message.ID, eventID)
		return
	}
	if err != nil {
		log.Printf("async image queue load failed: event=%s err=%v", eventID, err)
		return
	}
	var job AsyncImageJob
	if err := json.Unmarshal(payload, &job); err != nil || job.EventID != eventID {
		log.Printf("async image queue payload invalid: event=%s err=%v", eventID, err)
		if !q.handleUnrecoverableJob(eventID, fmt.Errorf("async image job payload is invalid: %v", err)) {
			return
		}
		q.finishMessage(message.ID, eventID)
		return
	}
	if job.Exhausted {
		if !q.finishExhaustedJob(job) {
			return
		}
		q.finishMessage(message.ID, eventID)
		return
	}

	job.Attempts++
	jobCtx, cancelJob := context.WithTimeout(context.Background(), asyncImageQueueExecutionTimeout)
	outcome := q.invokeHandler(jobCtx, job)
	cancelJob()
	if outcome.retry && outcome.preserveAttempt && job.Attempts > 0 {
		job.Attempts--
	}
	if outcome.retry && job.Attempts < q.maxAttempts {
		delay := outcome.retryAfter
		if delay <= 0 {
			delay = asyncImageQueueRetryDelay(job.Attempts)
		}
		if err := q.scheduleRetry(message.ID, job, delay); err != nil {
			log.Printf("async image queue retry scheduling failed: event=%s attempt=%d err=%v", eventID, job.Attempts, err)
		}
		return
	}
	if outcome.retry {
		job.Exhausted = true
		if outcome.err != nil {
			job.LastError = outcome.err.Error()
		}
		if err := q.persistJob(job); err != nil {
			log.Printf("async image queue exhaustion state failed: event=%s err=%v", eventID, err)
			return
		}
		if !q.finishExhaustedJob(job) {
			return
		}
	}
	if outcome.err != nil {
		log.Printf("async image job finished with error: event=%s attempt=%d retry=%t err=%v", eventID, job.Attempts, outcome.retry, outcome.err)
	}
	q.finishMessage(message.ID, eventID)
}

func (q *AsyncImageQueue) persistJob(job AsyncImageJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return q.redis.Set(ctx, q.jobKey(job.EventID), payload, 0).Err()
}

func (q *AsyncImageQueue) finishExhaustedJob(job AsyncImageJob) bool {
	if q.exhausted == nil {
		return true
	}
	var cause error
	if strings.TrimSpace(job.LastError) != "" {
		cause = errors.New(job.LastError)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	err := q.exhausted(ctx, job, cause)
	cancel()
	if err != nil {
		log.Printf("async image queue exhaustion handling failed: event=%s err=%v", job.EventID, err)
		return false
	}
	return true
}

func (q *AsyncImageQueue) heartbeatPending(messageID, consumer string, stop <-chan struct{}) {
	if q.heartbeatEvery <= 0 {
		return
	}
	ticker := time.NewTicker(q.heartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := q.redis.XClaimJustID(ctx, &redis.XClaimArgs{
				Stream:   asyncImageQueueStream,
				Group:    asyncImageQueueGroup,
				Consumer: consumer,
				MinIdle:  0,
				Messages: []string{messageID},
			}).Result()
			cancel()
			if err != nil && !errors.Is(err, redis.Nil) {
				log.Printf("async image queue heartbeat failed: message=%s consumer=%s err=%v", messageID, consumer, err)
			}
		}
	}
}

func (q *AsyncImageQueue) handleUnrecoverableJob(eventID string, cause error) bool {
	if q.exhausted == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	err := q.exhausted(ctx, AsyncImageJob{Version: 1, EventID: eventID}, cause)
	cancel()
	if err != nil {
		log.Printf("async image queue unrecoverable-job handling failed: event=%s err=%v", eventID, err)
		return false
	}
	return true
}

func (q *AsyncImageQueue) invokeHandler(ctx context.Context, job AsyncImageJob) (out asyncImageJobOutcome) {
	defer func() {
		if recovered := recover(); recovered != nil {
			out = asyncImageJobOutcome{err: fmt.Errorf("async image worker panic: %v", recovered), retry: true}
		}
	}()
	return q.handle(ctx, job)
}

func asyncImageQueueRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(1<<(attempt-1)) * time.Second
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func (q *AsyncImageQueue) scheduleRetry(messageID string, job AsyncImageJob, delay time.Duration) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	due := time.Now().Add(delay).UnixMilli()
	return retryAsyncImageScript.Run(ctx, q.redis,
		[]string{q.jobKey(job.EventID), asyncImageQueueScheduled, asyncImageQueueStream},
		payload, due, job.EventID, asyncImageQueueGroup, messageID).Err()
}

func (q *AsyncImageQueue) finishMessage(messageID, eventID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := finishAsyncImageScript.Run(ctx, q.redis,
		[]string{q.jobKey(eventID), asyncImageQueueScheduled, asyncImageQueueStream},
		asyncImageQueueGroup, messageID, eventID).Err(); err != nil {
		log.Printf("async image queue ack failed: event=%s message=%s err=%v", eventID, messageID, err)
	}
}

func (q *AsyncImageQueue) runPromoter(ctx context.Context) {
	ticker := time.NewTicker(q.promoteEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := promoteAsyncImagesScript.Run(ctx, q.redis,
				[]string{asyncImageQueueScheduled, asyncImageQueueStream},
				time.Now().UnixMilli(), asyncImageQueuePromoteBatch, asyncImageQueueJobKeyPrefix).Int(); err != nil && ctx.Err() == nil {
				log.Printf("async image queue promote failed: %v", err)
			}
		}
	}
}

func (q *AsyncImageQueue) runReclaimer(ctx context.Context) {
	ticker := time.NewTicker(q.reclaimEvery)
	defer ticker.Stop()
	consumer := q.consumerID + "-reclaim"
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			start := "0-0"
			for ctx.Err() == nil {
				messages, next, err := q.redis.XAutoClaim(ctx, &redis.XAutoClaimArgs{
					Stream:   asyncImageQueueStream,
					Group:    asyncImageQueueGroup,
					Consumer: consumer,
					MinIdle:  q.claimIdle,
					Start:    start,
					Count:    10,
				}).Result()
				if err != nil {
					if ctx.Err() == nil {
						log.Printf("async image queue reclaim failed: %v", err)
					}
					break
				}
				for _, message := range messages {
					q.processMessage(message, consumer)
				}
				if next == "0-0" || len(messages) == 0 {
					break
				}
				start = next
			}
		}
	}
}
