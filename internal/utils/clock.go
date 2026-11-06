// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package utils

import "time"

// Clock is the library's source of time for behavior that depends on it: the
// current instant and the tickers background loops wait on. The rebalancing
// code (internal/rebalancing and the lazy and incremental rebalancers in
// internal/structures) reads time only through a Clock, so a test can inject
// one that moves only when the test advances it (package clocktest) and no
// test depends on how fast the machine runs. Timestamps that are only logged
// or reported may still use the time package directly.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// NewTicker returns a ticker that ticks every d, as time.NewTicker does.
	// d must be positive.
	NewTicker(d time.Duration) Ticker
}

// Ticker delivers periodic ticks, as time.Ticker does.
type Ticker interface {
	// C returns the channel the ticks are delivered on.
	C() <-chan time.Time
	// Stop turns the ticker off: no tick is delivered after Stop returns.
	Stop()
}

// RealClock implements Clock with the system clock.
type RealClock struct{}

// Now returns time.Now().
func (RealClock) Now() time.Time {
	return time.Now()
}

// NewTicker returns a ticker backed by time.NewTicker.
func (RealClock) NewTicker(d time.Duration) Ticker {
	return realTicker{ticker: time.NewTicker(d)}
}

// realTicker adapts *time.Ticker to Ticker.
type realTicker struct {
	ticker *time.Ticker
}

// C returns the ticker's channel.
func (t realTicker) C() <-chan time.Time {
	return t.ticker.C
}

// Stop stops the ticker.
func (t realTicker) Stop() {
	t.ticker.Stop()
}
