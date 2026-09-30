package tasktest

import (
	"fmt"
	"testing"
	"time"

	"github.com/raiich/kazura/task"
)

// BenchmarkDispatcher runs the common Dispatcher benchmarks. Each Dispatcher
// implementation calls this from its own package benchmark, passing a
// SetupFunc, so the results of the implementations line up by name.
//
// Each benchmark measures one timer while a number of other timers stay
// pending, so the results show how the cost scales with the queue length.
func BenchmarkDispatcher(b *testing.B, setup SetupFunc) {
	b.Run("AfterFunc", func(b *testing.B) {
		benchAfterFunc(b, setup)
	})
	b.Run("Stop", func(b *testing.B) {
		benchStop(b, setup)
	})
}

// pendingCounts are the numbers of timers kept pending while a benchmark runs.
var pendingCounts = []int{0, 100, 1000}

// delay is the delay of the timer under measurement.
const delay = time.Millisecond

// benchAfterFunc measures scheduling one timer and advancing the time to fire it.
func benchAfterFunc(b *testing.B, setup SetupFunc) {
	for _, pending := range pendingCounts {
		b.Run(fmt.Sprintf("pending=%d", pending), func(b *testing.B) {
			d, h := setup(b)
			keepPending(d, pending)
			now := h.Start
			b.ReportAllocs()
			for b.Loop() {
				now = now.Add(delay)
				d.AfterFunc(delay, func() {})
				if err := h.AdvanceTo(now); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// benchStop measures scheduling one timer and stopping it before it fires.
func benchStop(b *testing.B, setup SetupFunc) {
	for _, pending := range pendingCounts {
		b.Run(fmt.Sprintf("pending=%d", pending), func(b *testing.B) {
			d, _ := setup(b)
			keepPending(d, pending)
			b.ReportAllocs()
			for b.Loop() {
				if !d.AfterFunc(delay, func() {}).Stop() {
					b.Fatal("Stop reported that the function ran")
				}
			}
		})
	}
}

// keepPending schedules n timers due far after the time the benchmark advances
// to, so they stay pending for the whole benchmark.
func keepPending(d task.Dispatcher, n int) {
	for range n {
		d.AfterFunc(100*365*24*time.Hour, func() {})
	}
}
