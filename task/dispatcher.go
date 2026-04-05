package task

import "time"

// Dispatcher defines the interface for scheduling delayed function execution.
// This abstraction allows working with different timer implementations,
// including test-friendly dispatchers that can control time simulation.
//
// AfterFunc is safe for concurrent use from multiple goroutines.
type Dispatcher interface {
	// AfterFunc schedules f to be executed after duration d and returns a [Timer]
	// that can cancel the scheduled execution.
	// Zero or negative d causes immediate scheduling (equivalent to d == 0).
	//
	// Synchronization: all scheduled functions are executed serially.
	// No additional synchronization is needed within callbacks, and shared
	// variable access is safe across callbacks. Functions with different delays
	// execute in delay order; for equal delays the order is unspecified.
	//
	// Panic handling: if f panics, the panic is caught and all remaining
	// scheduled functions are not executed. How the panic value is
	// reported is implementation-dependent.
	//
	// Example:
	//
	//	var counter int
	//	d.AfterFunc(1*time.Millisecond, func() { counter += 2 })
	//	d.AfterFunc(1*time.Millisecond, func() { counter += 3 })
	//	// After execution: counter == 5 (no race conditions)
	AfterFunc(d time.Duration, f func()) Timer
}
