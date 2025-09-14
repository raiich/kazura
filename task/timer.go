package task

// Timer represents a timer that can be stopped, similar to time.Timer from Go's standard library.
// This interface provides an abstraction for schedulable tasks that can be cancelled before execution.
type Timer interface {
	// Stop prevents the Timer from firing. It returns true if the call stops the timer,
	// false if the timer has already expired or been stopped.
	// This method is safe for concurrent use.
	Stop() bool
}
