// Package state provides a finite state machine driven by typed events. A
// transition runs the exit action of the state being left and then the Entry of
// the destination, and the machine acts on the [Command] that Entry returns; timer
// callbacks run between transitions.
package state

// State is one state of the machine. T is the type of the value the machine carries.
type State[T any] interface {
	// Entry is called when the machine enters this state with the handle of that
	// visit and the causing event (nil on [Machine.Launch]). It returns what the
	// machine does once Entry returns: [Trigger] to process an event, [Stop] to
	// stop the machine, or nil to stay.
	Entry(machine *EntryMachine[T], event Event) Command
}

// Command is what Entry asks the machine to do once it returns: nil stays in the
// state, and the values of [Trigger] and [Stop] do what they name. Any other
// value, such as a type embedding Command, is a failure of the chain.
type Command interface {
	command()
}

// Trigger returns the Command that processes event as [Machine.Trigger] would.
func Trigger(event Event) Command {
	return triggerCommand{event: event}
}

// triggerCommand is the Command that processes an event.
type triggerCommand struct {
	event Event
}

func (triggerCommand) command() {}

// Stop returns the Command that stops the machine as [Machine.Stop] would.
func Stop() Command {
	return stopCommand{}
}

// stopCommand is the Command that stops the machine.
type stopCommand struct{}

func (stopCommand) command() {}

// Event is what triggers a transition. Any type is an Event.
type Event interface {
}

// Guarded represents an error that can prevent state transitions.
// When returned by an exit action, it blocks the transition and keeps the machine in the current state.
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
