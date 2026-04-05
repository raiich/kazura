package task

// Timer represents a stoppable scheduled task, similar to [time.Timer].
type Timer interface {
	// Stop prevents the scheduled function from executing. It returns true if
	// the call successfully prevents execution, false if the function has
	// already been executed or Stop has already been called.
	//
	// Unlike [time.Timer.Stop], Stop returns true as long as the function has
	// not yet been executed by the [Dispatcher], even if the timer's duration
	// has already elapsed. This means Stop called from within a dispatched
	// callback can still cancel a pending function.
	//
	// Stop is safe for concurrent use.
	Stop() bool
}
