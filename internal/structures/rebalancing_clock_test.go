// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"fmt"
	"testing"
	"time"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/scigolib/hdf5/internal/utils/clocktest"
)

var clockTestStart = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// addUnderflowNodes queues node addresses for the incremental rebalancer, as
// a multi-level tree would on underflow (the MVP single-leaf tree never
// does). It writes the lazy state the way the library's own methods do,
// under bt.lazyMu.
func (bt *WritableBTreeV2) addUnderflowNodes(addrs ...uint64) {
	bt.lazyMu.Lock()
	defer bt.lazyMu.Unlock()
	bt.lazyState.UnderflowNodes = append(bt.lazyState.UnderflowNodes, addrs...)
}

// TestLazyRebalancing_MaxDelayFiresAtThresholdNotBefore drives the MaxDelay
// trigger with a mock clock: it is false one nanosecond short of MaxDelay,
// true at MaxDelay exactly, and false again once BatchRebalance has moved
// LastRebalance to the clock's present. The underflow ratio stays at zero,
// so only the delay can fire.
func TestLazyRebalancing_MaxDelayFiresAtThresholdNotBefore(t *testing.T) {
	t.Parallel()

	const maxDelay = time.Minute
	clock := clocktest.NewMockClock(clockTestStart)
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(LazyRebalancingConfig{
		Enabled:   true,
		Threshold: 0.05,
		MaxDelay:  maxDelay,
		BatchSize: 100,
		Clock:     clock,
	})
	bt.lazyState.UnderflowCount = 0
	bt.lazyState.TotalNodes = 100

	if !bt.lazyState.LastRebalance.Equal(clockTestStart) {
		t.Fatalf("LastRebalance = %v, want the clock's start %v", bt.lazyState.LastRebalance, clockTestStart)
	}

	clock.Advance(maxDelay - time.Nanosecond)
	if bt.shouldTriggerBatchRebalancing() {
		t.Fatal("triggered 1ns before MaxDelay")
	}
	if _, _, since := bt.GetLazyRebalancingStats(); since != maxDelay-time.Nanosecond {
		t.Errorf("time since rebalance = %v, want %v", since, maxDelay-time.Nanosecond)
	}

	clock.Advance(time.Nanosecond)
	if !bt.shouldTriggerBatchRebalancing() {
		t.Fatal("did not trigger at MaxDelay")
	}

	bt.lazyState.PendingDeletes = 7
	if err := bt.BatchRebalance(); err != nil {
		t.Fatalf("BatchRebalance: %v", err)
	}
	if want := clockTestStart.Add(maxDelay); !bt.lazyState.LastRebalance.Equal(want) {
		t.Errorf("LastRebalance after batch = %v, want %v", bt.lazyState.LastRebalance, want)
	}
	if bt.lazyState.PendingDeletes != 0 {
		t.Errorf("PendingDeletes after batch = %d, want 0", bt.lazyState.PendingDeletes)
	}
	if bt.shouldTriggerBatchRebalancing() {
		t.Error("triggered right after BatchRebalance")
	}

	clock.Advance(maxDelay)
	if !bt.shouldTriggerBatchRebalancing() {
		t.Error("did not trigger one MaxDelay after BatchRebalance")
	}
}

// TestLazyRebalancing_DefaultClockIsReal checks that a config without a clock
// gets the system clock, and that a state built without one (as older code
// and tests do) still reads the system clock instead of panicking.
func TestLazyRebalancing_DefaultClockIsReal(t *testing.T) {
	t.Parallel()

	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	before := time.Now()
	bt.EnableLazyRebalancing(DefaultLazyConfig())
	after := time.Now()

	if _, ok := bt.lazyState.Config.Clock.(utils.RealClock); !ok {
		t.Fatalf("default clock is %T, want utils.RealClock", bt.lazyState.Config.Clock)
	}
	if last := bt.lazyState.LastRebalance; last.Before(before) || last.After(after) {
		t.Errorf("LastRebalance = %v, not within [%v, %v]", last, before, after)
	}

	bare := &LazyRebalancingState{Config: LazyRebalancingConfig{MaxDelay: time.Hour}, LastRebalance: time.Now()}
	if _, ok := bare.clock().(utils.RealClock); !ok {
		t.Errorf("state without a clock uses %T, want utils.RealClock", bare.clock())
	}
	if bare.sinceLastRebalance() < 0 {
		t.Error("negative time since a past rebalance")
	}
}

// TestIncrementalRebalancing_RunsExactlyWhenAdvanced drives the background
// loop with a mock clock. No session runs before one Interval has passed;
// each Interval after that runs exactly one session; a session with no
// underflow nodes reports nothing and leaves LastRebalance alone.
//
// The progress callback runs on the loop's goroutine, so it may refill the
// underflow list without a data race; each refill gives the next tick work
// and therefore one more callback. The test reads the tree's state only
// after StopIncrementalRebalancing, which waits for the loop to exit.
func TestIncrementalRebalancing_RunsExactlyWhenAdvanced(t *testing.T) {
	t.Parallel()

	const interval = 5 * time.Second
	clock := clocktest.NewMockClock(clockTestStart)
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(LazyRebalancingConfig{Enabled: true, Clock: clock})
	bt.lazyState.UnderflowNodes = []uint64{1, 2}
	bt.lazyState.UnderflowCount = 1

	progress := make(chan RebalancingProgress, 16)
	sessions := 0
	err := bt.EnableIncrementalRebalancing(IncrementalRebalancingConfig{
		Enabled:  true,
		Budget:   time.Millisecond,
		Interval: interval,
		ProgressCallback: func(p RebalancingProgress) {
			sessions++ // only the loop goroutine touches it
			if sessions < 3 {
				bt.addUnderflowNodes(3, 4)
			}
			progress <- p
		},
	})
	if err != nil {
		t.Fatalf("EnableIncrementalRebalancing: %v", err)
	}
	clock.AwaitTicker(1) // the loop is running and waits on its ticker

	// Short of one interval the ticker does not fire, so no session can run.
	clock.Advance(interval - time.Nanosecond)
	if n := len(progress); n != 0 {
		t.Fatalf("sessions before one interval: %d, want 0", n)
	}

	// Each further interval delivers one tick; the session it starts drains
	// the two queued nodes (the mock's time stands still inside a session,
	// so the budget never cuts it short) and reports once.
	clock.Advance(time.Nanosecond)
	for i, wantTotal := range []int{2, 4, 6} {
		if i > 0 {
			clock.Advance(interval)
		}
		p := <-progress
		if p.NodesRebalanced != wantTotal {
			t.Errorf("session %d: NodesRebalanced = %d, want %d", i+1, p.NodesRebalanced, wantTotal)
		}
		if p.SessionDuration != 0 {
			t.Errorf("session %d: SessionDuration = %v, want 0 under the mock clock", i+1, p.SessionDuration)
		}
	}

	// Two more intervals: the ticks are received (Advance returns only then),
	// but there is no work, so no session reports and nothing is reset.
	clock.Advance(interval)
	clock.Advance(interval)

	if err := bt.StopIncrementalRebalancing(); err != nil {
		t.Fatalf("StopIncrementalRebalancing: %v", err)
	}
	if n := len(progress); n != 0 {
		t.Errorf("extra sessions reported: %d, want 0", n)
	}
	if sessions != 3 {
		t.Errorf("sessions with work = %d, want 3", sessions)
	}
	if n := len(bt.lazyState.UnderflowNodes); n != 0 {
		t.Errorf("underflow nodes left = %d, want 0", n)
	}
	if bt.lazyState.UnderflowCount != 0 {
		t.Errorf("UnderflowCount = %d, want 0 after the queue drained", bt.lazyState.UnderflowCount)
	}
	// The third session (at three intervals) drained the queue and stamped
	// LastRebalance; the two empty sessions after it did not.
	if want := clockTestStart.Add(3 * interval); !bt.lazyState.LastRebalance.Equal(want) {
		t.Errorf("LastRebalance = %v, want %v", bt.lazyState.LastRebalance, want)
	}
}

// TestIncrementalRebalancing_DeleteWhileRunning deletes records on one
// goroutine while the incremental loop runs sessions on its own. Both touch
// the tree's lazy-rebalancing state (pending deletes, underflow tracking,
// LastRebalance), so under -race this test proves that every access to that
// state is synchronized. The progress callback refills the underflow queue on
// the loop goroutine, so every tick runs a session that writes the state.
func TestIncrementalRebalancing_DeleteWhileRunning(t *testing.T) {
	t.Parallel()

	const (
		records  = 40
		interval = time.Second
	)
	clock := clocktest.NewMockClock(clockTestStart)
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	for i := range records {
		if err := bt.InsertRecord(fmt.Sprintf("rec_%03d", i), uint64(i+1)); err != nil {
			t.Fatalf("InsertRecord %d: %v", i, err)
		}
	}
	bt.EnableLazyRebalancing(LazyRebalancingConfig{Enabled: true, MaxDelay: time.Hour, Clock: clock})
	err := bt.EnableIncrementalRebalancing(IncrementalRebalancingConfig{
		Enabled:  true,
		Interval: interval,
		ProgressCallback: func(RebalancingProgress) {
			bt.addUnderflowNodes(1, 2)
		},
	})
	if err != nil {
		t.Fatalf("EnableIncrementalRebalancing: %v", err)
	}
	clock.AwaitTicker(1)
	bt.addUnderflowNodes(1, 2)

	deleted := make(chan error, 1)
	go func() {
		for i := range records / 2 {
			if err := bt.DeleteRecordLazy(fmt.Sprintf("rec_%03d", i)); err != nil {
				deleted <- fmt.Errorf("DeleteRecordLazy %d: %w", i, err)
				return
			}
			bt.GetLazyRebalancingStats()
		}
		deleted <- nil
	}()
	for range records {
		clock.Advance(interval)
	}
	if err := <-deleted; err != nil {
		t.Fatal(err)
	}

	if err := bt.StopIncrementalRebalancing(); err != nil {
		t.Fatalf("StopIncrementalRebalancing: %v", err)
	}
	if got := len(bt.GetRecords()); got != records/2 {
		t.Errorf("records left = %d, want %d", got, records/2)
	}
}

// TestIncrementalRebalancing_StopAndDisableDuringSession holds a session
// inside its progress callback and checks that no lock is held there: the
// lazy-state methods, DisableLazyRebalancing among them, return while the
// session is paused, and StopIncrementalRebalancing returns once the session
// ends. The session then finds the lazy state gone and must not touch it. A
// deadlock hangs the test until go test's -timeout reports every stack.
func TestIncrementalRebalancing_StopAndDisableDuringSession(t *testing.T) {
	t.Parallel()

	const interval = time.Second
	clock := clocktest.NewMockClock(clockTestStart)
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(LazyRebalancingConfig{Enabled: true, Clock: clock})

	inSession := make(chan RebalancingProgress)
	release := make(chan struct{})
	err := bt.EnableIncrementalRebalancing(IncrementalRebalancingConfig{
		Enabled:  true,
		Interval: interval,
		ProgressCallback: func(p RebalancingProgress) {
			inSession <- p
			<-release
		},
	})
	if err != nil {
		t.Fatalf("EnableIncrementalRebalancing: %v", err)
	}
	clock.AwaitTicker(1)
	bt.addUnderflowNodes(7, 8, 9)

	clock.Advance(interval)
	if p := <-inSession; p.NodesRebalanced != 3 || !p.IsComplete {
		t.Fatalf("progress in session = %+v, want 3 nodes rebalanced and complete", p)
	}

	// The session is paused in the callback; the tree must stay usable. The
	// session drained the queue, and it resets the lazy stats before the
	// callback (all its tree work ends there), so no time has passed since.
	if _, _, since := bt.GetLazyRebalancingStats(); since != 0 {
		t.Errorf("time since rebalance = %v, want 0", since)
	}
	if err := bt.ForceBatchRebalance(); err != nil {
		t.Errorf("ForceBatchRebalance during a session: %v", err)
	}
	if _, err := bt.GetIncrementalRebalancingProgress(); err != nil {
		t.Errorf("GetIncrementalRebalancingProgress during a session: %v", err)
	}
	if err := bt.DisableLazyRebalancing(); err != nil {
		t.Fatalf("DisableLazyRebalancing during a session: %v", err)
	}

	stopped := make(chan error)
	go func() { stopped <- bt.StopIncrementalRebalancing() }()
	close(release)
	if err := <-stopped; err != nil {
		t.Fatalf("StopIncrementalRebalancing: %v", err)
	}
	if bt.IsLazyRebalancingEnabled() || bt.IsIncrementalRebalancingEnabled() {
		t.Error("rebalancing still enabled after Disable and Stop")
	}
}

// TestIncrementalRebalancing_StopFromProgressCallback calls
// StopIncrementalRebalancing from inside the progress callback, on the loop's
// own goroutine. Stop must signal the loop and return instead of waiting for
// the goroutine it runs on; the loop then exits once the callback returns.
//
// A regression deadlocks, so the wait is bounded by the test's own deadline:
// with -timeout the test fails with a message before go test kills the
// binary, and without one it waits like the code under test would.
func TestIncrementalRebalancing_StopFromProgressCallback(t *testing.T) {
	t.Parallel()

	const interval = time.Second
	clock := clocktest.NewMockClock(clockTestStart)
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(LazyRebalancingConfig{Enabled: true, Clock: clock})

	stopResult := make(chan error, 1)
	err := bt.EnableIncrementalRebalancing(IncrementalRebalancingConfig{
		Enabled:  true,
		Interval: interval,
		ProgressCallback: func(RebalancingProgress) {
			stopResult <- bt.StopIncrementalRebalancing()
		},
	})
	if err != nil {
		t.Fatalf("EnableIncrementalRebalancing: %v", err)
	}
	rebalancer := bt.incrementalRebalancer
	ticker := clock.AwaitTicker(1)
	bt.addUnderflowNodes(1)

	clock.Advance(interval)

	var hang <-chan time.Time
	if deadline, ok := t.Deadline(); ok {
		timer := time.NewTimer(time.Until(deadline) * 9 / 10)
		defer timer.Stop()
		hang = timer.C
	}
	select {
	case err := <-stopResult:
		if err != nil {
			t.Fatalf("StopIncrementalRebalancing from the callback: %v", err)
		}
	case <-hang:
		t.Fatal("StopIncrementalRebalancing called from the progress callback deadlocked: it waits for its own goroutine")
	}

	// The loop exits after the callback returns and stops its ticker.
	<-ticker.Stopped()
	if bt.IsIncrementalRebalancingEnabled() {
		t.Error("incremental rebalancing still enabled after Stop from the callback")
	}
	// A later Stop on the same rebalancer, from another goroutine, waits for
	// the (already finished) loop and returns; it must not close the stop
	// channel twice.
	rebalancer.Stop()
	clock.Advance(time.Hour) // must not block: the ticker is stopped
}

// TestIncrementalRebalancing_NoWorkAfterStopReturns pins Stop's guarantee:
// once Stop has returned, the rebalancer reads and writes no tree state and
// runs no further session, even when Stop came from another goroutine while
// the progress callback was running and so returned before the loop exited.
// The test changes the lazy state after Stop returns and checks, once the
// loop is gone, that the rebalancer left it alone.
func TestIncrementalRebalancing_NoWorkAfterStopReturns(t *testing.T) {
	t.Parallel()

	const interval = time.Second
	clock := clocktest.NewMockClock(clockTestStart)
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(LazyRebalancingConfig{Enabled: true, MaxDelay: time.Hour, Clock: clock})

	inSession := make(chan struct{})
	release := make(chan struct{})
	callbacks := 0 // written by the loop goroutine, read after it exits
	err := bt.EnableIncrementalRebalancing(IncrementalRebalancingConfig{
		Enabled:  true,
		Interval: interval,
		ProgressCallback: func(RebalancingProgress) {
			callbacks++
			inSession <- struct{}{}
			<-release
		},
	})
	if err != nil {
		t.Fatalf("EnableIncrementalRebalancing: %v", err)
	}
	rebalancer := bt.incrementalRebalancer
	ticker := clock.AwaitTicker(1)
	bt.addUnderflowNodes(1, 2)

	clock.Advance(interval)
	<-inSession // the session drained both nodes and is paused in the callback

	// Stop from this goroutine while the callback runs. It cannot wait for
	// the loop (the callback may be the caller), so it returns at once.
	rebalancer.Stop()

	// From here on the tree is the caller's alone. Put state in place that a
	// late session or its bookkeeping would change: queued nodes it would
	// drain, counters a "queue drained" reset would zero. The writes are
	// deliberately unlocked: the guarantee is that the rebalancer no longer
	// touches the tree at all, so -race flags any access it still makes.
	bt.lazyState.UnderflowCount = 5
	bt.lazyState.PendingDeletes = 5
	lastRebalance := clockTestStart.Add(-time.Hour)
	bt.lazyState.LastRebalance = lastRebalance

	// Queue a tick that is ready when the loop returns from the callback, so
	// the loop's select sees both the tick and the stop signal.
	advanced := make(chan struct{})
	go func() {
		defer close(advanced)
		clock.Advance(interval)
	}()
	close(release)
	<-ticker.Stopped() // the loop has exited (it stops its ticker on the way out)
	<-advanced

	bt.addUnderflowNodes(3, 4)
	clock.Advance(interval) // the ticker is stopped: nothing is delivered

	if callbacks != 1 {
		t.Errorf("sessions reported = %d, want 1", callbacks)
	}
	if p := rebalancer.GetProgress(); p.NodesRebalanced != 2 {
		t.Errorf("nodes rebalanced = %d, want 2 (no work after Stop)", p.NodesRebalanced)
	}
	underflow, pending, _ := bt.GetLazyRebalancingStats()
	if underflow != 5 || pending != 5 {
		t.Errorf("after Stop the rebalancer changed the lazy state: UnderflowCount = %d, PendingDeletes = %d, want 5 and 5", underflow, pending)
	}
	bt.lazyMu.Lock()
	gotLast, queued := bt.lazyState.LastRebalance, len(bt.lazyState.UnderflowNodes)
	bt.lazyMu.Unlock()
	if !gotLast.Equal(lastRebalance) {
		t.Errorf("LastRebalance = %v, want %v (as set after Stop)", gotLast, lastRebalance)
	}
	if queued != 2 {
		t.Errorf("queued underflow nodes = %d, want 2 (none drained after Stop)", queued)
	}
	if err := bt.StopIncrementalRebalancing(); err != nil {
		t.Fatalf("StopIncrementalRebalancing: %v", err)
	}
}

// TestIncrementalRebalancing_StopEndsTheLoop checks that Stop ends the loop
// and stops its ticker, so later Advance calls deliver nothing.
func TestIncrementalRebalancing_StopEndsTheLoop(t *testing.T) {
	t.Parallel()

	clock := clocktest.NewMockClock(clockTestStart)
	bt := NewWritableBTreeV2(DefaultBTreeV2NodeSize)
	bt.EnableLazyRebalancing(LazyRebalancingConfig{Enabled: true, Clock: clock})
	if err := bt.EnableIncrementalRebalancing(IncrementalRebalancingConfig{Enabled: true, Interval: time.Second}); err != nil {
		t.Fatalf("EnableIncrementalRebalancing: %v", err)
	}
	ticker := clock.AwaitTicker(1)

	if err := bt.StopIncrementalRebalancing(); err != nil {
		t.Fatalf("StopIncrementalRebalancing: %v", err)
	}
	select {
	case <-ticker.Stopped():
	default:
		t.Fatal("ticker still running after StopIncrementalRebalancing returned")
	}
	clock.Advance(time.Hour) // must not block: nobody receives, the ticker is stopped
	if bt.IsIncrementalRebalancingEnabled() {
		t.Error("still enabled after Stop")
	}
}
