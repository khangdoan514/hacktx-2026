package metrics

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewLimiterRejectsInvalidCaps(t *testing.T) {
	if _, err := NewLimiter(0, nil); !errors.Is(err, ErrLimitInvalid) {
		t.Fatalf("zero default = %v", err)
	}
	if _, err := NewLimiter(1, map[string]int{"org-a": 0}); !errors.Is(err, ErrLimitInvalid) {
		t.Fatalf("zero override = %v", err)
	}
	if _, err := NewLimiter(1, map[string]int{" org-a": 1}); !errors.Is(err, ErrOrgRequired) {
		t.Fatalf("blank org = %v", err)
	}
}

func TestAcquireRejectsMissingIdentity(t *testing.T) {
	lim, err := NewLimiter(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lim.Acquire(nil, "org-a"); !errors.Is(err, ErrNilContext) {
		t.Fatalf("nil context = %v", err)
	}
	if _, err := lim.Acquire(context.Background(), ""); !errors.Is(err, ErrOrgRequired) {
		t.Fatalf("empty org = %v", err)
	}
	if _, err := lim.Acquire(context.Background(), " org-a"); !errors.Is(err, ErrOrgRequired) {
		t.Fatalf("padded org = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lim.Acquire(ctx, "org-a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %v", err)
	}
	if lim.InFlight("org-a") != 0 {
		t.Fatalf("in flight = %d", lim.InFlight("org-a"))
	}
	admitted, rejected := lim.Stats()
	if admitted != 0 || rejected != 0 {
		t.Fatalf("admitted %d rejected %d", admitted, rejected)
	}
}

func TestOrgConcurrencyLimit(t *testing.T) {
	overrides := map[string]int{"org-b": 2}
	lim, err := NewLimiter(1, overrides)
	if err != nil {
		t.Fatal(err)
	}
	overrides["org-b"] = 99

	holdA, err := lim.Acquire(context.Background(), "org-a")
	if err != nil {
		t.Fatal(err)
	}
	if lim.InFlight("org-a") != 1 || lim.Limit("org-a") != 1 {
		t.Fatalf("org-a in flight %d limit %d", lim.InFlight("org-a"), lim.Limit("org-a"))
	}
	deniedCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := lim.Acquire(deniedCtx, "org-a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second org-a acquire = %v", err)
	}

	holdB1, err := lim.Acquire(context.Background(), "org-b")
	if err != nil {
		t.Fatal(err)
	}
	holdB2, err := lim.Acquire(context.Background(), "org-b")
	if err != nil {
		t.Fatal(err)
	}
	if lim.InFlight("org-b") != 2 || lim.Limit("org-b") != 2 {
		t.Fatalf("org-b in flight %d limit %d", lim.InFlight("org-b"), lim.Limit("org-b"))
	}
	deniedB, cancelB := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelB()
	if _, err := lim.Acquire(deniedB, "org-b"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third org-b acquire = %v", err)
	}

	admitted, rejected := lim.Stats()
	if admitted != 3 || rejected != 2 {
		t.Fatalf("admitted %d rejected %d", admitted, rejected)
	}
	holdA()
	holdA()
	holdB1()
	holdB2()
	if lim.InFlight("org-a") != 0 || lim.InFlight("org-b") != 0 {
		t.Fatalf("after release a=%d b=%d", lim.InFlight("org-a"), lim.InFlight("org-b"))
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	lim, err := NewLimiter(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	release, err := lim.Acquire(context.Background(), "org-a")
	if err != nil {
		t.Fatal(err)
	}
	release()
	done := make(chan struct{})
	go func() {
		release()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second release blocked")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	again, err := lim.Acquire(ctx, "org-a")
	if err != nil {
		t.Fatal(err)
	}
	again()
	if lim.InFlight("org-a") != 0 {
		t.Fatalf("in flight = %d", lim.InFlight("org-a"))
	}
}

func TestParallelInFlightNeverExceedsLimit(t *testing.T) {
	const limit = 3
	const workers = 12
	lim, err := NewLimiter(limit, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var inflight atomic.Int32
	var maxInflight atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, err := lim.Acquire(ctx, "org-a")
			if err != nil {
				t.Error(err)
				return
			}
			n := inflight.Add(1)
			noteMax(&maxInflight, n)
			inflight.Add(-1)
			release()
		}()
	}
	close(start)
	wg.Wait()
	if got := maxInflight.Load(); got > int32(limit) {
		t.Fatalf("in flight %d exceeded limit %d", got, limit)
	}
	if lim.InFlight("org-a") != 0 {
		t.Fatalf("in flight after release = %d", lim.InFlight("org-a"))
	}
	admitted, rejected := lim.Stats()
	if admitted != int64(workers) || rejected != 0 {
		t.Fatalf("admitted %d rejected %d", admitted, rejected)
	}

	held := make(chan struct{})
	releaseAll := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(limit)
	for i := 0; i < limit; i++ {
		go func() {
			release, err := lim.Acquire(context.Background(), "org-a")
			if err != nil {
				t.Error(err)
				ready.Done()
				return
			}
			ready.Done()
			<-releaseAll
			release()
			held <- struct{}{}
		}()
	}
	ready.Wait()
	if lim.InFlight("org-a") != limit {
		t.Fatalf("saturated in flight = %d", lim.InFlight("org-a"))
	}
	blocked, blockCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer blockCancel()
	if _, err := lim.Acquire(blocked, "org-a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire at cap = %v", err)
	}
	close(releaseAll)
	for i := 0; i < limit; i++ {
		select {
		case <-held:
		case <-time.After(time.Second):
			t.Fatal("holder did not release")
		}
	}
	if lim.InFlight("org-a") != 0 {
		t.Fatalf("in flight after holders = %d", lim.InFlight("org-a"))
	}
}

func noteMax(max *atomic.Int32, n int32) {
	for {
		old := max.Load()
		if n <= old || max.CompareAndSwap(old, n) {
			return
		}
	}
}
