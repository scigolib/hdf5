//go:build race

package rebalancing

// raceEnabled reports whether the binary was built with the race detector.
// Timing assertions are meaningless under its instrumentation: every recorded
// operation costs an order of magnitude more, so such tests skip.
const raceEnabled = true
