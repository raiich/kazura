// Package task provides the [Dispatcher] abstraction that serializes function
// execution, together with the [Task] and [Timer] handles its methods return.
package task

import (
	"context"
	"errors"
)

// ErrCanceled reports that the dispatcher stopped without running a function
// submitted via [Dispatcher.InvokeFunc]. Match it with [errors.Is].
var ErrCanceled = errors.New("task canceled")

// Task is a handle for awaiting completion of a function submitted via
// [Dispatcher.InvokeFunc].
type Task interface {
	// Wait blocks until the submitted function finishes. If ctx is done first, it
	// returns context.Cause(ctx) without canceling the function; otherwise it
	// returns nil. If the dispatcher stopped without running the function, it
	// returns an error matching [ErrCanceled]. If the function panicked, Wait
	// re-panics with the same value on every call. Wait is safe for concurrent use.
	//
	// Calling Wait from within a function dispatched by the same dispatcher
	// may deadlock, depending on the dispatcher.
	Wait(ctx context.Context) error
}
