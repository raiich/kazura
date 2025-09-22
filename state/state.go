package state

// State represents a state in the finite state machine.
// T is the type of data that the state machine manages.
type State[T any] interface {
	// Entry is called when the state machine transitions into this state (entry-action).
	// The event parameter contains the event that triggered this state transition.
	Entry(machine *EntryMachine[T], event Event)
}

// Event represents an event that can trigger state transitions by Machine.Trigger method.
// Any type can be used as an event by implementing this interface.
type Event interface {
}

// Guarded represents an error that can prevent state transitions.
// When returned by an exit-action, it blocks the transition and keeps the machine in the current state.
type Guarded struct {
	// Reason describes why the state transition was blocked.
	Reason error
}

// Error returns the error message describing why the state transition was blocked.
func (g *Guarded) Error() string {
	if g.Reason == nil {
		return "state transition blocked"
	}
	return g.Reason.Error()
}
