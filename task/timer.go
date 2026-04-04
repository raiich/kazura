package task

// Timer represents a stoppable scheduled task, similar to [time.Timer].
type Timer interface {
	// Stop prevents the scheduled function from executing. It returns true if
	// the call successfully prevents execution, false if the function has
	// already been executed or Stop has already been called.
	//
	// In dispatcher-based implementations, Stop returns true as long as the
	// dispatched function has not yet been executed by the dispatcher, even if
	// the underlying [time.AfterFunc] has already fired. This is because the
	// dispatcher serializes execution — if the callback is waiting to be
	// dispatched, Stop called from within a dispatched task can still prevent it.
	//
	// Stop is safe for concurrent use.
	Stop() bool
}
