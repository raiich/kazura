package state

// Option configures a Machine at construction time.
// S is the state type of the Machine the option applies to.
type Option[S any] func(*machineConfig[S])

// machineConfig holds construction-time configuration that depends only on
// the Machine's state type parameter S.
type machineConfig[S any] struct {
	tracer Tracer[S]
}

// WithTracer configures the Machine to notify the given Tracer on every
// state transition. See Tracer for the call semantics.
func WithTracer[S any](t Tracer[S]) Option[S] {
	return func(c *machineConfig[S]) {
		c.tracer = t
	}
}

// Tracer observes state transitions for logging or debugging purposes.
// S is the state type used by the Machine.
//
// Trace is called after an exit-action (if any) succeeds and before the
// Entry method of the destination state is invoked.
//
// Special cases:
//   - On the initial transition triggered by Launch, fromState is the zero
//     value of S and event is nil.
//   - On Stop, fromState is the state the machine was in, toState is the
//     zero value of S, and event is nil.
//
// Trace is not called when the transition is blocked by a Guarded error
// returned from an exit-action, or when Stop is invoked from within an
// exit-action (the destination-side Trace for the blocked transition is
// suppressed; the Stop-side Trace is still recorded).
//
// Trace is invoked synchronously on the Machine's goroutine. Implementations
// must not block; long-running work should be offloaded.
type Tracer[S any] interface {
	Trace(fromState, toState S, event Event)
}
