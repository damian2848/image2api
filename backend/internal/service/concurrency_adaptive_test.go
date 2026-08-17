package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func testConcurrencyService(t *testing.T) (*ConcurrencyService, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewConcurrencyService(client), server
}

func TestAdaptiveLimitRecoversAndBacksOff(t *testing.T) {
	service, redisServer := testConcurrencyService(t)
	ctx := context.Background()
	const (
		limitKey    = "test:limit"
		successKey  = "test:success"
		overloadKey = "test:overload"
	)

	if got := service.AdaptiveLimit(ctx, limitKey, 4, 1, 16, time.Hour); got != 4 {
		t.Fatalf("initial limit = %d, want 4", got)
	}
	for i := 0; i < 19; i++ {
		if got := service.RecordAdaptiveSuccess(ctx, limitKey, successKey, 4, 1, 16, 20, time.Hour); got != 4 {
			t.Fatalf("limit after %d successes = %d, want 4", i+1, got)
		}
	}
	if got := service.RecordAdaptiveSuccess(ctx, limitKey, successKey, 4, 1, 16, 20, time.Hour); got != 5 {
		t.Fatalf("limit after recovery threshold = %d, want 5", got)
	}

	if limit, count := service.RecordAdaptiveOverload(ctx, limitKey, successKey, overloadKey, 4, 1, 16, 10*time.Second, time.Hour); limit != 4 || count != 1 {
		t.Fatalf("first overload = (limit %d, count %d), want (4, 1)", limit, count)
	}
	if limit, count := service.RecordAdaptiveOverload(ctx, limitKey, successKey, overloadKey, 4, 1, 16, 10*time.Second, time.Hour); limit != 3 || count != 2 {
		t.Fatalf("second overload = (limit %d, count %d), want (3, 2)", limit, count)
	}

	redisServer.FastForward(11 * time.Second)
	if _, count := service.RecordAdaptiveOverload(ctx, limitKey, successKey, overloadKey, 4, 1, 16, 10*time.Second, time.Hour); count != 1 {
		t.Fatalf("overload count after window = %d, want 1", count)
	}
}

func TestAdobeRenderBucketsAreIndependent(t *testing.T) {
	service, _ := testConcurrencyService(t)
	v1 := &V1Service{conc: service}
	ctx := context.Background()

	threePLimitKey := adobeSubmitKey(adobeRenderLimitKeyPrefix, adobeSubmitBucket3P)
	imageV5LimitKey := adobeSubmitKey(adobeRenderLimitKeyPrefix, adobeSubmitBucketImageV5)
	if err := service.redis.Set(ctx, threePLimitKey, adobeRenderMinConcurrency, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if err := service.redis.Set(ctx, imageV5LimitKey, adobeRenderMinConcurrency, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}

	threeP := make([]*adobeRenderLease, 0, adobeRenderMinConcurrency)
	for index := 0; index < adobeRenderMinConcurrency; index++ {
		lease, err := v1.acquireAdobeRenderLease(ctx, fmt.Sprintf("evt-first-%d", index), adobeSubmitBucket3P)
		if err != nil {
			t.Fatal(err)
		}
		threeP = append(threeP, lease)
	}
	defer func() {
		for _, lease := range threeP {
			lease.finish(false)
		}
	}()

	timedCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := v1.acquireAdobeRenderLease(timedCtx, "evt-blocked", adobeSubmitBucket3P); err == nil {
		t.Fatal("second 3p-images lease should wait for the occupied bucket")
	}

	imageV5, err := v1.acquireAdobeRenderLease(ctx, "evt-image-v5", adobeSubmitBucketImageV5)
	if err != nil {
		t.Fatalf("image-v5 bucket should remain independent: %v", err)
	}
	imageV5.finish(false)
}

func TestAdobeRenderLeaseIsHeldUntilJobFinishes(t *testing.T) {
	service, _ := testConcurrencyService(t)
	v1 := &V1Service{conc: service}
	ctx := context.Background()
	limitKey := adobeSubmitKey(adobeRenderLimitKeyPrefix, adobeSubmitBucket3P)
	if err := service.redis.Set(ctx, limitKey, adobeRenderMinConcurrency, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}

	running := make([]*adobeRenderLease, 0, adobeRenderMinConcurrency)
	for index := 0; index < adobeRenderMinConcurrency; index++ {
		lease, err := v1.acquireAdobeRenderLease(ctx, fmt.Sprintf("evt-running-%d", index), adobeSubmitBucket3P)
		if err != nil {
			t.Fatal(err)
		}
		running = append(running, lease)
	}

	acquired := make(chan *adobeRenderLease, 1)
	go func() {
		next, acquireErr := v1.acquireAdobeRenderLease(ctx, "evt-next", adobeSubmitBucket3P)
		if acquireErr == nil {
			acquired <- next
		}
	}()
	select {
	case next := <-acquired:
		next.finish(false)
		t.Fatal("next render entered before the running job completed")
	case <-time.After(250 * time.Millisecond):
	}
	running[0].finish(false)
	select {
	case next := <-acquired:
		next.finish(false)
	case <-time.After(time.Second):
		t.Fatal("next render did not enter after the running job completed")
	}
	for _, lease := range running[1:] {
		lease.finish(false)
	}
}

func TestAdobeRenderGateBoundsTwentyRequestBurst(t *testing.T) {
	service, _ := testConcurrencyService(t)
	v1 := &V1Service{conc: service}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var active atomic.Int32
	var maximum atomic.Int32
	var group sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			lease, err := v1.acquireAdobeRenderLease(ctx, fmt.Sprintf("evt-%02d", index), adobeSubmitBucket3P)
			if err != nil {
				errors <- err
				return
			}
			current := active.Add(1)
			for {
				seen := maximum.Load()
				if current <= seen || maximum.CompareAndSwap(seen, current) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
			lease.finish(false)
		}(i)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("burst admission failed: %v", err)
	}
	if got := maximum.Load(); got > adobeRenderInitialConcurrency {
		t.Fatalf("maximum simultaneous renders = %d, want <= %d", got, adobeRenderInitialConcurrency)
	}
	if got := maximum.Load(); got < 2 {
		t.Fatalf("burst did not exercise concurrency: maximum = %d", got)
	}
}

func TestAdobeSubmitGateSerializesOneStickySession(t *testing.T) {
	service, _ := testConcurrencyService(t)
	v1 := &V1Service{conc: service}
	ctx := context.Background()

	first, err := v1.acquireAdobeSubmitLease(ctx, "evt-first", adobeSubmitBucket3P, "img1")
	if err != nil {
		t.Fatal(err)
	}
	timedCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := v1.acquireAdobeSubmitLease(timedCtx, "evt-same", adobeSubmitBucket3P, "img1"); err == nil {
		t.Fatal("same sticky session submitted concurrently")
	}

	other, err := v1.acquireAdobeSubmitLease(ctx, "evt-other", adobeSubmitBucket3P, "img2")
	if err != nil {
		t.Fatalf("independent sticky session was blocked: %v", err)
	}
	var releases sync.WaitGroup
	releases.Add(2)
	go func() {
		defer releases.Done()
		first.finish(nil)
	}()
	go func() {
		defer releases.Done()
		other.finish(nil)
	}()
	releases.Wait()
	if count := service.Count(ctx, adobeProxySubmitKeyPrefix+"img1"); count != 0 {
		t.Fatalf("sticky submit slot leaked: %d", count)
	}
}

func TestQueuedAdobeRenderHonorsReducedLimit(t *testing.T) {
	service, _ := testConcurrencyService(t)
	v1 := &V1Service{conc: service}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	leases := make([]*adobeRenderLease, 0, adobeRenderInitialConcurrency)
	for i := 0; i < adobeRenderInitialConcurrency; i++ {
		lease, err := v1.acquireAdobeRenderLease(ctx, fmt.Sprintf("evt-held-%d", i), adobeSubmitBucket3P)
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, lease)
	}

	acquired := make(chan *adobeRenderLease, 1)
	failed := make(chan error, 1)
	go func() {
		lease, err := v1.acquireAdobeRenderLease(ctx, "evt-queued", adobeSubmitBucket3P)
		if err != nil {
			failed <- err
			return
		}
		acquired <- lease
	}()

	limitKey := adobeSubmitKey(adobeRenderLimitKeyPrefix, adobeSubmitBucket3P)
	successKey := adobeSubmitKey(adobeRenderSuccessKeyPrefix, adobeSubmitBucket3P)
	overloadKey := adobeSubmitKey(adobeRenderOverloadKeyPrefix, adobeSubmitBucket3P)
	service.RecordAdaptiveOverload(ctx, limitKey, successKey, overloadKey,
		adobeRenderInitialConcurrency, adobeRenderMinConcurrency, adobeRenderMaxConcurrency,
		adobeOverloadWindow, time.Hour)

	// Releasing one live lease leaves exactly the reduced limit active. A queued
	// worker must not enter using the old limit.
	leases[0].finish(false)
	select {
	case lease := <-acquired:
		lease.finish(false)
		t.Fatal("queued worker ignored the reduced adaptive limit")
	case err := <-failed:
		t.Fatalf("queued worker failed early: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	leases[1].finish(false)
	select {
	case lease := <-acquired:
		lease.finish(false)
	case err := <-failed:
		t.Fatalf("queued worker failed after capacity was released: %v", err)
	case <-time.After(time.Second):
		t.Fatal("queued worker did not enter at the reduced limit")
	}
	for _, lease := range leases[2:] {
		lease.finish(false)
	}
}

func TestAdobeOverloadRetryRejoinsReducedRenderGate(t *testing.T) {
	service, _ := testConcurrencyService(t)
	v1 := &V1Service{conc: service}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	renderPermit, cancelUnused, err := v1.primedAdobeRenderPermit(ctx, "evt-retry", adobeSubmitBucket3P)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelUnused()
	firstAttempt, err := renderPermit(ctx)
	if err != nil {
		t.Fatal(err)
	}

	other := make([]*adobeRenderLease, 0, adobeRenderInitialConcurrency-1)
	for i := 0; i < adobeRenderInitialConcurrency-1; i++ {
		lease, acquireErr := v1.acquireAdobeRenderLease(ctx, fmt.Sprintf("evt-other-%d", i), adobeSubmitBucket3P)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		other = append(other, lease)
	}

	limitKey := adobeSubmitKey(adobeRenderLimitKeyPrefix, adobeSubmitBucket3P)
	successKey := adobeSubmitKey(adobeRenderSuccessKeyPrefix, adobeSubmitBucket3P)
	overloadKey := adobeSubmitKey(adobeRenderOverloadKeyPrefix, adobeSubmitBucket3P)
	service.RecordAdaptiveOverload(ctx, limitKey, successKey, overloadKey,
		adobeRenderInitialConcurrency, adobeRenderMinConcurrency, adobeRenderMaxConcurrency,
		adobeOverloadWindow, time.Hour)
	firstAttempt.finish(false)

	retried := make(chan *adobeRenderLease, 1)
	go func() {
		lease, acquireErr := renderPermit(ctx)
		if acquireErr == nil {
			retried <- lease
		}
	}()
	select {
	case lease := <-retried:
		lease.finish(false)
		t.Fatal("retry entered while active renders were still at the reduced limit")
	case <-time.After(250 * time.Millisecond):
	}

	other[0].finish(false)
	select {
	case lease := <-retried:
		lease.finish(false)
	case <-time.After(time.Second):
		t.Fatal("retry did not re-enter after reduced render capacity became available")
	}
	for _, lease := range other[1:] {
		lease.finish(false)
	}
}
