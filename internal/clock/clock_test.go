package clock

import (
	"sync"
	"testing"
	"time"
)

func assertNoNotification(t *testing.T, timer Timer) {
	t.Helper()

	select {
	case got, ok := <-timer.Notification():
		t.Fatalf("unexpected notification: time=%v, open=%v", got, ok)
	default:
	}
}

func assertNotification(t *testing.T, timer Timer, want time.Time) {
	t.Helper()

	select {
	case got, ok := <-timer.Notification():
		if !ok {
			t.Fatal("notification channel unexpectedly closed")
		}
		if !got.Equal(want) {
			t.Fatalf("notification = %v, want %v", got, want)
		}
	default:
		t.Fatal("expected notification")
	}
}

func TestRealNow(t *testing.T) {
	var clock Real

	before := time.Now()
	got := clock.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Fatalf("Now() = %v, outside [%v, %v]", got, before, after)
	}
}

func TestRealNewTimer(t *testing.T) {
	for _, duration := range []time.Duration{
		-time.Second,
		0,
		time.Millisecond,
	} {
		t.Run(duration.String(), func(t *testing.T) {
			var clock Real
			timer := clock.NewTimer(duration)
			defer timer.Stop()

			ch := timer.Notification()
			if ch == nil || ch != timer.Notification() {
				t.Fatal("Notification() must return a stable, non-nil channel")
			}

			select {
			case _, ok := <-ch:
				if !ok {
					t.Fatal("notification channel unexpectedly closed")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timer did not fire")
			}

			assertNoNotification(t, timer)
			if timer.Stop() {
				t.Fatal("Stop() after receiving notification = true, want false")
			}
		})
	}
}

func TestNativeTimerAdapterStop(t *testing.T) {
	var clock Real
	timer := clock.NewTimer(time.Hour)
	defer timer.Stop()

	if !timer.Stop() {
		t.Fatal("first Stop() = false, want true")
	}
	if timer.Stop() {
		t.Fatal("second Stop() = true, want false")
	}
	assertNoNotification(t, timer)
}

func TestNewFakeAndNow(t *testing.T) {
	for _, start := range []time.Time{
		{},
		time.Unix(-1, 123),
		time.Date(2026, time.November, 20, 12, 0, 0, 123, time.UTC),
	} {
		t.Run(start.String(), func(t *testing.T) {
			clock := NewFake(start)
			if got := clock.Now(); got != start {
				t.Fatalf("Now() = %v, want %v", got, start)
			}
			if got := clock.Now(); got != start {
				t.Fatalf("repeated Now() = %v, want %v", got, start)
			}
		})
	}
}

func TestFakeZeroValue(t *testing.T) {
	var clock Fake
	start := time.Time{}

	if got := clock.Now(); got != start {
		t.Fatalf("zero-value Now() = %v, want %v", got, start)
	}

	timer := clock.NewTimer(time.Second)
	clock.Advance(time.Second)
	assertNotification(t, timer, start.Add(time.Second))
}

func TestFakeNewTimer(t *testing.T) {
	start := time.Unix(100, 0)

	for _, duration := range []time.Duration{
		-time.Hour,
		-time.Nanosecond,
		0,
		time.Nanosecond,
		time.Hour,
		time.Duration(1<<63 - 1),
	} {
		t.Run(duration.String(), func(t *testing.T) {
			clock := NewFake(start)
			timer := clock.NewTimer(duration)

			if ch := timer.Notification(); ch == nil ||
				ch != timer.Notification() {
				t.Fatal("Notification() must return a stable, non-nil channel")
			}

			// Fake timers are delivered by Advance, including overdue timers.
			assertNoNotification(t, timer)
			if got := clock.Now(); got != start {
				t.Fatalf("NewTimer changed Now() to %v", got)
			}

			if duration > 0 {
				clock.Advance(duration - time.Nanosecond)
				assertNoNotification(t, timer)
				clock.Advance(time.Nanosecond)
			} else {
				clock.Advance(0)
			}

			assertNotification(t, timer, clock.Now())

			clock.Advance(time.Second)
			assertNoNotification(t, timer)
		})
	}
}

func TestFakeAdvance(t *testing.T) {
	start := time.Unix(100, 0)
	clock := NewFake(start)

	// Creation order intentionally differs from deadline order.
	late := clock.NewTimer(3 * time.Second)
	early := clock.NewTimer(time.Second)
	sameDeadline := clock.NewTimer(time.Second)
	middle := clock.NewTimer(2 * time.Second)

	clock.Advance(time.Second)
	assertNotification(t, early, start.Add(time.Second))
	assertNotification(t, sameDeadline, start.Add(time.Second))
	assertNoNotification(t, middle)
	assertNoNotification(t, late)

	// A jump past a deadline reports the advanced clock time.
	clock.Advance(3 * time.Second)
	want := start.Add(4 * time.Second)
	assertNotification(t, middle, want)
	assertNotification(t, late, want)

	clock.Advance(time.Hour)
	for _, timer := range []Timer{early, sameDeadline, middle, late} {
		assertNoNotification(t, timer)
	}
}

func TestFakeAdvanceWithoutTimers(t *testing.T) {
	start := time.Unix(100, 0)
	clock := NewFake(start)

	for _, duration := range []time.Duration{
		0, time.Hour, -2 * time.Hour, time.Nanosecond,
	} {
		start = start.Add(duration)
		clock.Advance(duration)
		if got := clock.Now(); got != start {
			t.Fatalf("after Advance(%v), Now() = %v, want %v",
				duration, got, start)
		}
	}
}

func TestFakeNegativeAdvanceKeepsFutureTimerPending(t *testing.T) {
	start := time.Unix(100, 0)
	clock := NewFake(start)
	timer := clock.NewTimer(time.Second)

	clock.Advance(-time.Second)
	assertNoNotification(t, timer)
	clock.Advance(time.Second)
	assertNoNotification(t, timer)
	clock.Advance(time.Second)
	assertNotification(t, timer, start.Add(time.Second))
}

func TestPendingTimerStop(t *testing.T) {
	for _, duration := range []time.Duration{-time.Second, 0, time.Second} {
		t.Run(duration.String(), func(t *testing.T) {
			clock := NewFake(time.Unix(100, 0))
			timer := clock.NewTimer(duration)

			if !timer.Stop() {
				t.Fatal("first Stop() = false, want true")
			}
			if timer.Stop() {
				t.Fatal("second Stop() = true, want false")
			}

			clock.Advance(2 * time.Second)
			assertNoNotification(t, timer)
		})
	}
}

func TestFakeAdvanceWithUnreadNotification(t *testing.T) {
	start := time.Unix(100, 0)
	clock := NewFake(start)
	timer := clock.NewTimer(time.Second)

	clock.Advance(time.Second)
	// Leaving the notification unread must not cause another delivery.
	clock.Advance(time.Hour)

	assertNotification(t, timer, start.Add(time.Second))
	assertNoNotification(t, timer)
}

func TestSortTimers(t *testing.T) {
	for _, deadlines := range [][]int64{
		nil,
		{},
		{1},
		{1, 2, 3},
		{3, 2, 1},
		{2, 1, 2, 1},
	} {
		timers := make([]*pendingTimer, len(deadlines))
		original := make(map[*pendingTimer]bool, len(deadlines))
		for i, deadline := range deadlines {
			timer := &pendingTimer{deadline: time.Unix(deadline, 0)}
			timers[i] = timer
			original[timer] = true
		}

		sortTimers(timers)

		for i, timer := range timers {
			if !original[timer] {
				t.Fatal("sort duplicated or introduced a timer")
			}
			delete(original, timer)
			if i > 0 && timers[i-1].deadline.After(timer.deadline) {
				t.Fatalf("deadlines out of order at index %d", i)
			}
		}
		if len(original) != 0 {
			t.Fatal("sort lost timers")
		}
	}
}

func runConcurrent(workers int, fn func(int)) {
	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start
			fn(i)
		}()
	}
	close(start)
	wg.Wait()
}

func TestFakeConcurrentNowNewTimerAndAdvance(t *testing.T) {
	start := time.Unix(100, 0)
	clock := NewFake(start)

	const workers = 32
	const iterations = 100

	runConcurrent(workers, func(_ int) {
		for i := 0; i < iterations; i++ {
			clock.Now()
			clock.NewTimer(0).Notification()
			clock.Advance(time.Nanosecond)
		}
	})

	want := start.Add(workers * iterations * time.Nanosecond)
	if got := clock.Now(); got != want {
		t.Fatalf("Now() = %v, want %v", got, want)
	}

	clock.Advance(0)
	if len(clock.pendingTimers) != 0 {
		t.Fatalf("%d expired timers remain", len(clock.pendingTimers))
	}
}

func TestNativeTimerAdapterConcurrentStopAndNotification(t *testing.T) {
	var clock Real
	timer := clock.NewTimer(time.Hour)
	defer timer.Stop()
	ch := timer.Notification()

	const workers = 32
	results := make(chan bool, workers)

	runConcurrent(workers, func(_ int) {
		if timer.Notification() != ch {
			t.Error("Notification() returned a different channel")
		}
		results <- timer.Stop()
	})
	close(results)

	stopped := 0
	for result := range results {
		if result {
			stopped++
		}
	}
	if stopped != 1 {
		t.Fatalf("%d successful Stop() calls, want 1", stopped)
	}
	assertNoNotification(t, timer)
}

// Run with -race: Stop accesses pendingTimer.stopped without synchronization.
func TestPendingTimerConcurrentStop(t *testing.T) {
	clock := NewFake(time.Unix(100, 0))
	timer := clock.NewTimer(time.Hour)

	runConcurrent(32, func(_ int) {
		for i := 0; i < 100; i++ {
			timer.Notification()
			timer.Stop()
		}
	})

	if timer.Stop() {
		t.Fatal("Stop() after concurrent stops = true, want false")
	}
	assertNoNotification(t, timer)
}

// Advance's mutex does not synchronize with pendingTimer.Stop.
func TestPendingTimerConcurrentStopAndAdvance(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		clock := NewFake(time.Unix(100, 0))
		timer := clock.NewTimer(time.Second)

		runConcurrent(2, func(worker int) {
			if worker == 0 {
				timer.Stop()
			} else {
				clock.Advance(time.Second)
			}
		})

		// Either firing or stopping may win. Delivery must remain one-shot.
		select {
		case _, ok := <-timer.Notification():
			if !ok {
				t.Fatal("notification channel unexpectedly closed")
			}
		default:
		}
		clock.Advance(time.Second)
		assertNoNotification(t, timer)
	}
}
