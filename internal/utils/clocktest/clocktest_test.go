// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package clocktest

import (
	"testing"
	"time"

	"github.com/scigolib/hdf5/internal/utils"
)

// Both clocks satisfy the interface the library takes.
var (
	_ utils.Clock  = (*MockClock)(nil)
	_ utils.Clock  = utils.RealClock{}
	_ utils.Ticker = (*MockTicker)(nil)
)

// TestMockClock_TickerFiresOnlyOnAdvance pins the fake's contract that the
// rebalancer tests rely on: no tick before a full period, one tick per period
// passed, none after Stop.
func TestMockClock_TickerFiresOnlyOnAdvance(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewMockClock(start)
	ticker := clock.NewTicker(time.Minute)

	got := make(chan time.Time, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case at := <-ticker.C():
				got <- at
			case <-ticker.(*MockTicker).Stopped():
				return
			}
		}
	}()

	// Short of one period nothing is delivered at all, so the receiver has
	// nothing to forward: the check needs no waiting.
	clock.Advance(time.Minute - time.Nanosecond)
	if n := len(got); n != 0 {
		t.Fatalf("ticks before one period: got %d, want 0", n)
	}

	// Two periods in total. Advance returns once both ticks were received;
	// the blocking reads below take what the receiver forwarded.
	clock.Advance(time.Minute + time.Nanosecond)
	if at := <-got; !at.Equal(start.Add(time.Minute)) {
		t.Errorf("first tick at %v, want %v", at, start.Add(time.Minute))
	}
	if at := <-got; !at.Equal(start.Add(2 * time.Minute)) {
		t.Errorf("second tick at %v, want %v", at, start.Add(2*time.Minute))
	}

	ticker.Stop()
	ticker.Stop() // idempotent
	<-done
	clock.Advance(time.Hour) // must not block: the ticker is stopped
	if n := len(got); n != 0 {
		t.Errorf("extra ticks: got %d, want 0", n)
	}
	want := start.Add(time.Hour + 2*time.Minute)
	if !clock.Now().Equal(want) {
		t.Errorf("Now = %v, want %v", clock.Now(), want)
	}
}

// TestMockClock_AwaitTicker returns the n-th ticker once it exists, also when
// it is created after the wait began.
func TestMockClock_AwaitTicker(t *testing.T) {
	clock := NewMockClock(time.Unix(0, 0))
	first := clock.NewTicker(time.Second)
	if got := clock.AwaitTicker(1); got != first {
		t.Fatalf("AwaitTicker(1) = %p, want %p", got, first)
	}

	created := make(chan utils.Ticker)
	go func() { created <- clock.NewTicker(time.Second) }()
	second := clock.AwaitTicker(2)
	if want := <-created; second != want {
		t.Fatalf("AwaitTicker(2) = %p, want %p", second, want)
	}
}

// TestMockClock_NewTickerPanicsOnNonPositive matches time.NewTicker.
func TestMockClock_NewTickerPanicsOnNonPositive(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewTicker(0) did not panic")
		}
	}()
	NewMockClock(time.Unix(0, 0)).NewTicker(0)
}

// TestRealClock_Ticker checks the real adapter's wiring: Now lies between two
// reads of time.Now, and a stopped ticker exposes its channel.
func TestRealClock_Ticker(t *testing.T) {
	before := time.Now()
	now := utils.RealClock{}.Now()
	after := time.Now()
	if now.Before(before) || now.After(after) {
		t.Fatalf("RealClock.Now = %v, not within [%v, %v]", now, before, after)
	}

	ticker := utils.RealClock{}.NewTicker(time.Hour)
	if ticker.C() == nil {
		t.Fatal("RealClock ticker has a nil channel")
	}
	ticker.Stop()
}
