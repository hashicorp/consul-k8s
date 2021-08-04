package coalesce

import (
	"context"
	"testing"
	"time"
)

func TestCoalesce_quiet(t *testing.T) {
	total := 0
	deltaCh := make(chan int, 10)
	deltaCh <- 1
	deltaCh <- 39
	deltaCh <- 2

	start := time.Now()
	Coalesce(context.Background(),
		100*time.Millisecond,
		500*time.Millisecond,
		testSummer(&total, deltaCh))
	duration := time.Since(start)
	if total != 42 {
		t.Fatalf("total != 42: %d", total)
	}

	// We should complete in a quiet period
	if duration > 250*time.Millisecond {
		t.Fatalf("duration should be lower than max: %s", duration)
	}
}

func TestCoalesce_max(t *testing.T) {
	total := 0
	deltaCh := make(chan int, 10)
	go func() {
		for i := 0; i < 10; i++ {
			deltaCh <- 1
			time.Sleep(100 * time.Millisecond)
		}
	}()

	start := time.Now()
	Coalesce(context.Background(),
		200*time.Millisecond,
		500*time.Millisecond,
		testSummer(&total, deltaCh))
	duration := time.Since(start)
	if total < 4 || total > 6 {
		// 4 to 6 to account for CI weirdness
		t.Fatalf("total should be 4 to 6: %d", total)
	}

	// We should complete in the max period
	if duration < 500*time.Millisecond {
		t.Fatalf("duration should be greater than max: %s", duration)
	}
}

// Test that if the cancel function is called, Coalesce exits.
// We test this via having a function that just sleeps and then calling
// cancel on it. We expect that Coalesce exits
func TestCoalesce_cancel(t *testing.T) {
	total := 0
	deltaCh := make(chan int, 10)
	go func() {
		deltaCh <- 1
		select {}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel is called after only 50ms.
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	// Coalesce should be exited due to the cancel function because its
	// other timeouts are much higher.
	Coalesce(ctx,
		500*time.Millisecond,
		1000*time.Millisecond,
		testSummer(&total, deltaCh))
	duration := time.Since(start)
	// The check on total here isn't super important since it should
	// never fail but I kept it in to match the rest of the tests.
	if total != 1 {
		t.Fatalf("total should be 1 got: %d", total)
	}

	// We should complete in ~50ms but add 100 to account for timing.
	if duration > 150*time.Millisecond {
		t.Fatalf("duration should be less than 150ms: %s", duration)
	}
}

// testSummer returns a coalesce callback that sums all the ints sent to
// the given channel into the total.
func testSummer(total *int, ch <-chan int) func(context.Context) {
	return func(ctx context.Context) {
		select {
		case delta := <-ch:
			*total += delta
		case <-ctx.Done():
		}
	}
}
