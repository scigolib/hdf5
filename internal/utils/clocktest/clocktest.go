// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

// Package clocktest provides a utils.Clock for tests whose time moves only
// when the test says so. It is shared by the tests of every package that
// takes a utils.Clock, so there is one fake with one contract.
package clocktest

import (
	"sync"
	"time"

	"github.com/scigolib/hdf5/internal/utils"
)

// MockClock is a utils.Clock whose time moves only when the test calls
// Advance. The tickers it creates fire from Advance, never from wall-clock
// time, so a test that uses it gives the same result on an idle machine and
// on a loaded one.
//
// A tick is handed over synchronously: Advance returns only after the code
// under test has received every tick that fell due (or stopped the ticker).
// A time.Ticker drops ticks for a slow receiver instead; the mock delivers
// each one so that a test can count them exactly.
//
// MockClock is safe for concurrent use: the code under test reads Now and
// stops tickers from its own goroutines while the test advances the clock.
type MockClock struct {
	mu      sync.Mutex
	current time.Time
	created []*MockTicker // every ticker ever created, in creation order
	changed chan struct{} // closed and replaced when a ticker is created
}

// NewMockClock creates a mock clock starting at the given time.
func NewMockClock(start time.Time) *MockClock {
	return &MockClock{current: start, changed: make(chan struct{})}
}

// Now returns the current mock time.
func (m *MockClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// NewTicker returns a ticker that fires each time Advance moves the clock past
// one more period. Like time.NewTicker it panics on a non-positive period.
func (m *MockClock) NewTicker(d time.Duration) utils.Ticker {
	if d <= 0 {
		panic("clocktest: non-positive interval for MockClock.NewTicker")
	}
	t := &MockTicker{
		clock:   m,
		period:  d,
		c:       make(chan time.Time),
		stopped: make(chan struct{}),
	}

	m.mu.Lock()
	t.next = m.current.Add(d)
	m.created = append(m.created, t)
	close(m.changed)
	m.changed = make(chan struct{})
	m.mu.Unlock()

	return t
}

// Advance moves the mock time forward by d and delivers, in time order per
// ticker, every tick that fell due. It returns once each tick was received or
// its ticker was stopped.
func (m *MockClock) Advance(d time.Duration) {
	type due struct {
		ticker *MockTicker
		at     time.Time
	}

	m.mu.Lock()
	m.current = m.current.Add(d)
	now := m.current
	var ticks []due
	for _, t := range m.created {
		if t.isStopped {
			continue
		}
		for !t.next.After(now) {
			ticks = append(ticks, due{ticker: t, at: t.next})
			t.next = t.next.Add(t.period)
		}
	}
	m.mu.Unlock()

	for _, tick := range ticks {
		tick.ticker.deliver(tick.at)
	}
}

// AwaitTicker blocks until the clock has created at least n tickers and
// returns the n-th one (1-based). A goroutine that waits on a ticker has
// started once its ticker exists, so this replaces "sleep until the goroutine
// runs". It never times out: a ticker that is never created hangs the test
// until go test's -timeout reports it with every goroutine's stack.
func (m *MockClock) AwaitTicker(n int) *MockTicker {
	for {
		m.mu.Lock()
		if len(m.created) >= n {
			t := m.created[n-1]
			m.mu.Unlock()
			return t
		}
		changed := m.changed
		m.mu.Unlock()
		<-changed
	}
}

// MockTicker is the utils.Ticker MockClock creates.
type MockTicker struct {
	clock   *MockClock
	period  time.Duration
	c       chan time.Time
	stopped chan struct{} // closed by the first Stop

	// Guarded by clock.mu.
	next      time.Time
	isStopped bool
}

// C returns the tick channel. It is unbuffered: see MockClock.
func (t *MockTicker) C() <-chan time.Time {
	return t.c
}

// Stop turns the ticker off. It is idempotent.
func (t *MockTicker) Stop() {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.isStopped {
		return
	}
	t.isStopped = true
	close(t.stopped)
}

// Stopped returns a channel that is closed once the ticker is stopped. A loop
// that stops its ticker on exit (defer ticker.Stop()) has exited, or is about
// to, once this channel is closed.
func (t *MockTicker) Stopped() <-chan struct{} {
	return t.stopped
}

// deliver hands one tick to the receiver, or drops it if the ticker is
// stopped first.
func (t *MockTicker) deliver(at time.Time) {
	select {
	case t.c <- at:
	case <-t.stopped:
	}
}
