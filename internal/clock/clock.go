package clock

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type Timer interface {
	Notification() <-chan time.Time
	Stop() bool
}

type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}

// nativeTimerAdapter adapts *time.Timer to implement Timer.
type nativeTimerAdapter struct {
	inner *time.Timer
}

func (a *nativeTimerAdapter) Notification() <-chan time.Time {
	return a.inner.C
}

func (a *nativeTimerAdapter) Stop() bool {
	return a.inner.Stop()
}

type Real struct{}

func (r *Real) Now() time.Time {
	return time.Now()
}

func (r *Real) NewTimer(duration time.Duration) Timer {
	return &nativeTimerAdapter{
		inner: time.NewTimer(duration),
	}
}

type pendingTimer struct {
	deadline time.Time
	ch       chan time.Time
	stopped  atomic.Bool
}

func (p *pendingTimer) Notification() <-chan time.Time {
	return p.ch
}

// return true if not already stopped
func (p *pendingTimer) Stop() bool {
	return !p.stopped.Swap(true)
}

type Fake struct {
	currentTime   time.Time
	pendingTimers []*pendingTimer
	mu            sync.Mutex
}

func NewFake(start time.Time) *Fake {
	return &Fake{
		currentTime:   start,
		pendingTimers: make([]*pendingTimer, 0), // can remove it too
	}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.currentTime
}

func (f *Fake) NewTimer(duration time.Duration) Timer {
	f.mu.Lock()
	defer f.mu.Unlock()

	timer := &pendingTimer{
		deadline: f.currentTime.Add(duration),
		ch:       make(chan time.Time, 1),
	}

	f.pendingTimers = append(f.pendingTimers, timer)
	return timer
}

// moves time forward and fires every timer whose deadline has passed, in deadline order
func (f *Fake) Advance(duration time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.currentTime = f.currentTime.Add(duration)
	sortTimers(f.pendingTimers)

	unexpired := f.pendingTimers[:0] // trick instead of var unexpired []*pendingTimers
	for i, t := range f.pendingTimers {
		if t.deadline.After(f.currentTime) {
			unexpired = append(unexpired, f.pendingTimers[i:]...)
			break
		}
		if !t.stopped.Load() {
			select {
			case t.ch <- f.currentTime:
			default:
			}
		}
	}
	f.pendingTimers = unexpired
}

func sortTimers(timers []*pendingTimer) {
	slices.SortFunc(timers, func(a, b *pendingTimer) int {
		return a.deadline.Compare(b.deadline)
	})
}
