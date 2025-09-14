package state

import (
	"time"

	"github.com/raiich/kazura/task"
)

// Dispatcher defines the interface for scheduling delayed function execution.
// This abstraction allows the state manager to work with different timer implementations,
// including test-friendly dispatchers that can control time simulation.
type Dispatcher interface {
	// AfterFunc schedules a function to be executed after the specified duration.
	// Returns a Timer that can be used to cancel the scheduled execution.
	//
	// Synchronization Guarantee:
	// Functions registered with AfterFunc are executed with proper synchronization.
	// Even if multiple functions are scheduled to execute at the same time,
	// they will be executed serially without race conditions. This means:
	//   - No additional mutex/synchronization is needed within the callback functions
	//   - Shared variable access is safe between different callbacks
	//   - Execution order may be non-deterministic for simultaneous scheduled functions
	//
	// Example:
	//   var counter int = 0
	//   d.AfterFunc(1*time.Millisecond, func() { counter += 2 })
	//   d.AfterFunc(1*time.Millisecond, func() { counter += 3 })
	//   // After execution: counter will be 5 (no race conditions)
	AfterFunc(d time.Duration, f func()) task.Timer
}
