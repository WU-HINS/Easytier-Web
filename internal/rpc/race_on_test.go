//go:build race

package rpc

// The race detector instruments every memory access and slows hot paths by an order
// of magnitude. Timing budgets are about algorithmic blowup, not throughput, so they
// scale here instead of being skipped: a quadratic path still blows past 2s.
const raceOverheadFactor = 10
