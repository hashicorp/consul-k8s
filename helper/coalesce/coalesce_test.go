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
	duration := time.Now().Sub(start)
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
	duration := time.Now().Sub(start)
	if total < 4 || total > 6 {
		// 4 to 6 to account for CI weirdness
		t.Fatalf("total should be 4 to 6: %d", total)
	}

	// We should complete in the max period
	if duration < 500*time.Millisecond {
		t.Fatalf("duration should be greater than max: %s", duration)
	}
}

func TestCoalesce_cancel(t *testing.T) {
	total := 0
	deltaCh := make(chan int, 10)
	go func() {
		for i := 0; i < 10; i++ {
			deltaCh <- 1
			time.Sleep(50 * time.Millisecond)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	start := time.Now()
	Coalesce(ctx,
		100*time.Millisecond,
		500*time.Millisecond,
		testSummer(&total, deltaCh))
	duration := time.Now().Sub(start)
	if total < 3 || total > 5 {
		// 4 to 6 to account for CI weirdness
		t.Fatalf("total should be 3 to 5: %d", total)
	}

	// We should complete in the max period
	if duration > 300*time.Millisecond {
		t.Fatalf("duration should be less than 300ms: %s", duration)
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
