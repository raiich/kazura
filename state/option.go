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
// Trace is called after the exit action and before the Entry of the destination,
// and on Launch and Stop. A blocked transition or an event with no transition is
// not traced; it is returned to the caller.
//
// Trace runs synchronously on the Machine's goroutine, inside the transition, and
// must not block; Launch, Trigger and Stop return an error from it.
type Tracer[S any] interface {
	Trace(t Transition[S])
}

// Transition is one transition reported to a [Tracer]:
//
//   - From zero: the initial transition of [Machine.Launch].
//   - To zero: a stop; From is the last state and Event is nil. Guarded is the
//     guard the exit action returned and the stop overrode, if any.
type Transition[S any] struct {
	From, To S
	Event    Event
	Guarded  *Guarded
}
