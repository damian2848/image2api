package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func testAsyncImageRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client, server
}

func tuneAsyncImageQueueForTest(queue *AsyncImageQueue) {
	queue.readBlock = 10 * time.Millisecond
	queue.promoteEvery = 5 * time.Millisecond
	queue.reclaimEvery = time.Hour
	queue.heartbeatEvery = time.Hour
	queue.claimIdle = time.Hour
}

func TestAsyncImageQueueSurvivesWorkerRestart(t *testing.T) {
	client, _ := testAsyncImageRedis(t)
	producer := newAsyncImageQueue(client, 1, func(context.Context, AsyncImageJob) asyncImageJobOutcome {
		return asyncImageJobOutcome{}
	}, nil)
	job := AsyncImageJob{EventID: "evt-persist", Request: V1ImageRequest{Model: "image2", Prompt: "persist me"}}
	if err := producer.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	handled := make(chan AsyncImageJob, 1)
	restarted := newAsyncImageQueue(client, 1, func(_ context.Context, got AsyncImageJob) asyncImageJobOutcome {
		handled <- got
		return asyncImageJobOutcome{}
	}, nil)
	tuneAsyncImageQueueForTest(restarted)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := restarted.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-handled:
		if got.EventID != job.EventID || got.Request.Prompt != job.Request.Prompt {
			t.Fatalf("recovered job = %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("persisted job was not consumed after worker restart")
	}
	eventually(t, time.Second, func() bool {
		return client.Exists(context.Background(), restarted.jobKey(job.EventID)).Val() == 0
	})
}

func TestAsyncImageQueueBoundsTwentyJobBurst(t *testing.T) {
	client, _ := testAsyncImageRedis(t)
	var active atomic.Int32
	var maximum atomic.Int32
	var completed atomic.Int32
	done := make(chan struct{})
	queue := newAsyncImageQueue(client, 4, func(_ context.Context, _ AsyncImageJob) asyncImageJobOutcome {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
		active.Add(-1)
		if completed.Add(1) == 20 {
			close(done)
		}
		return asyncImageJobOutcome{}
	}, nil)
	tuneAsyncImageQueueForTest(queue)
	for i := 0; i < 20; i++ {
		if err := queue.Enqueue(context.Background(), AsyncImageJob{EventID: "evt-burst-" + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("burst did not drain")
	}
	if got := maximum.Load(); got > 4 {
		t.Fatalf("maximum concurrent workers = %d, want <= 4", got)
	} else if got < 2 {
		t.Fatalf("burst did not exercise worker concurrency: maximum = %d", got)
	}
}

func TestAsyncImageQueueDelayedRetryIsDurable(t *testing.T) {
	client, _ := testAsyncImageRedis(t)
	var mu sync.Mutex
	var attempts []int
	done := make(chan struct{})
	queue := newAsyncImageQueue(client, 1, func(_ context.Context, job AsyncImageJob) asyncImageJobOutcome {
		mu.Lock()
		attempts = append(attempts, job.Attempts)
		mu.Unlock()
		if job.Attempts == 1 {
			return asyncImageJobOutcome{err: errors.New("retry me"), retry: true, retryAfter: 10 * time.Millisecond}
		}
		close(done)
		return asyncImageJobOutcome{}
	}, nil)
	tuneAsyncImageQueueForTest(queue)
	if err := queue.Enqueue(context.Background(), AsyncImageJob{EventID: "evt-retry"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("delayed retry did not run")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 2 {
		t.Fatalf("attempts = %v, want [1 2]", attempts)
	}
}

func TestAsyncImageQueueContentionDoesNotConsumeAttempts(t *testing.T) {
	client, _ := testAsyncImageRedis(t)
	var calls atomic.Int32
	done := make(chan struct{})
	queue := newAsyncImageQueue(client, 1, func(_ context.Context, job AsyncImageJob) asyncImageJobOutcome {
		if job.Attempts != 1 {
			t.Errorf("contention attempt = %d, want 1", job.Attempts)
		}
		if calls.Add(1) <= 5 {
			return asyncImageJobOutcome{
				err:             ErrUserConcurrencyFull,
				retry:           true,
				retryAfter:      time.Millisecond,
				preserveAttempt: true,
			}
		}
		close(done)
		return asyncImageJobOutcome{}
	}, nil)
	tuneAsyncImageQueueForTest(queue)
	if err := queue.Enqueue(context.Background(), AsyncImageJob{EventID: "evt-contention"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("contention retries exhausted the job")
	}
	if got := calls.Load(); got != 6 {
		t.Fatalf("handler calls = %d, want 6", got)
	}
}

func TestAsyncImageQueueEnqueueIsIdempotent(t *testing.T) {
	client, _ := testAsyncImageRedis(t)
	queue := newAsyncImageQueue(client, 1, func(context.Context, AsyncImageJob) asyncImageJobOutcome {
		return asyncImageJobOutcome{}
	}, nil)
	first := AsyncImageJob{EventID: "evt-dedupe", Request: V1ImageRequest{Prompt: "first"}}
	second := AsyncImageJob{EventID: "evt-dedupe", Request: V1ImageRequest{Prompt: "second"}}
	if err := queue.Enqueue(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if got := client.XLen(context.Background(), asyncImageQueueStream).Val(); got != 1 {
		t.Fatalf("stream length = %d, want 1", got)
	}
	data, err := client.Get(context.Background(), queue.jobKey(first.EventID)).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var stored AsyncImageJob
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Request.Prompt != "first" {
		t.Fatalf("deduplicated payload prompt = %q, want first", stored.Request.Prompt)
	}
}

func TestAsyncImageQueueRejectsBeyondConfiguredDepth(t *testing.T) {
	client, _ := testAsyncImageRedis(t)
	queue := newAsyncImageQueue(client, 1, func(context.Context, AsyncImageJob) asyncImageJobOutcome {
		return asyncImageJobOutcome{}
	}, nil)
	queue.maxDepth = 1
	if err := queue.Enqueue(context.Background(), AsyncImageJob{EventID: "evt-capacity-1"}); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(context.Background(), AsyncImageJob{EventID: "evt-capacity-2"}); !errors.Is(err, ErrAsyncImageQueueFull) {
		t.Fatalf("second enqueue error = %v, want ErrAsyncImageQueueFull", err)
	}
}

func TestAsyncImageQueueCallsExhaustionHandler(t *testing.T) {
	client, _ := testAsyncImageRedis(t)
	exhausted := make(chan AsyncImageJob, 1)
	queue := newAsyncImageQueue(client, 1, func(_ context.Context, _ AsyncImageJob) asyncImageJobOutcome {
		return asyncImageJobOutcome{err: errors.New("still unavailable"), retry: true, retryAfter: time.Millisecond}
	}, func(_ context.Context, job AsyncImageJob, _ error) error {
		exhausted <- job
		return nil
	})
	queue.maxAttempts = 2
	tuneAsyncImageQueueForTest(queue)
	if err := queue.Enqueue(context.Background(), AsyncImageJob{EventID: "evt-exhaust"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case job := <-exhausted:
		if job.Attempts != 2 {
			t.Fatalf("exhausted attempts = %d, want 2", job.Attempts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exhaustion handler was not called")
	}
	eventually(t, time.Second, func() bool {
		return client.Exists(context.Background(), queue.jobKey("evt-exhaust")).Val() == 0
	})
}

func TestAsyncImageQueueExhaustedStateDoesNotRunHandlerAgain(t *testing.T) {
	client, _ := testAsyncImageRedis(t)
	var handlerCalls atomic.Int32
	var exhaustionCalls atomic.Int32
	firstExhaustion := make(chan struct{})
	queue := newAsyncImageQueue(client, 1, func(_ context.Context, _ AsyncImageJob) asyncImageJobOutcome {
		handlerCalls.Add(1)
		return asyncImageJobOutcome{err: errors.New("worker unavailable"), retry: true}
	}, func(_ context.Context, _ AsyncImageJob, _ error) error {
		if exhaustionCalls.Add(1) == 1 {
			close(firstExhaustion)
			return errors.New("database temporarily unavailable")
		}
		return nil
	})
	queue.maxAttempts = 1
	tuneAsyncImageQueueForTest(queue)
	if err := queue.Enqueue(context.Background(), AsyncImageJob{EventID: "evt-exhaust-state"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstExhaustion:
	case <-time.After(2 * time.Second):
		t.Fatal("first exhaustion callback did not run")
	}
	messages, err := client.XRangeN(context.Background(), asyncImageQueueStream, "-", "+", 1).Result()
	if err != nil || len(messages) != 1 {
		t.Fatalf("load pending exhausted message: messages=%v err=%v", messages, err)
	}
	queue.processMessage(messages[0], queue.consumerID+"-manual")
	if got := handlerCalls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1 after exhaustion retry", got)
	}
	if got := exhaustionCalls.Load(); got != 2 {
		t.Fatalf("exhaustion calls = %d, want 2", got)
	}
}

func TestAsyncImageQueueReclaimsCrashedConsumer(t *testing.T) {
	client, _ := testAsyncImageRedis(t)
	handled := make(chan AsyncImageJob, 1)
	queue := newAsyncImageQueue(client, 1, func(_ context.Context, job AsyncImageJob) asyncImageJobOutcome {
		handled <- job
		return asyncImageJobOutcome{}
	}, nil)
	tuneAsyncImageQueueForTest(queue)
	queue.reclaimEvery = 5 * time.Millisecond
	queue.claimIdle = time.Millisecond
	if err := client.XGroupCreateMkStream(context.Background(), asyncImageQueueStream, asyncImageQueueGroup, "0").Err(); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(context.Background(), AsyncImageJob{EventID: "evt-crashed"}); err != nil {
		t.Fatal(err)
	}
	streams, err := client.XReadGroup(context.Background(), &redis.XReadGroupArgs{
		Group:    asyncImageQueueGroup,
		Consumer: "dead-consumer",
		Streams:  []string{asyncImageQueueStream, ">"},
		Count:    1,
		Block:    -1,
	}).Result()
	if err != nil || len(streams) != 1 || len(streams[0].Messages) != 1 {
		t.Fatalf("claim into dead consumer: streams=%v err=%v", streams, err)
	}
	time.Sleep(5 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.runReclaimer(ctx)
	select {
	case job := <-handled:
		if job.EventID != "evt-crashed" {
			t.Fatalf("reclaimed event = %q", job.EventID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("crashed consumer job was not reclaimed")
	}
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition did not become true before timeout")
}
