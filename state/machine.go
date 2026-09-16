package state

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/raiich/kazura/state/graph"
	"github.com/raiich/kazura/task"
)

var (
	errNilGraph        = errors.New("graph is nil")
	errNotLaunched     = errors.New("machine is not launched")
	errAlreadyLaunched = errors.New("machine is already launched")
	errInTransition    = errors.New("machine is in a transition")
	errInCallback      = errors.New("machine is not launchable from a callback")
	errNoTransition    = errors.New("no transition found for event")
	errNilEvent        = errors.New("event cannot be nil")
	errUnknownCommand  = errors.New("unknown Command")
)

// NewMachine creates a new state machine with the given graph and value.
// Returns a configured but not yet launched machine. A nil graph is reported by
// [Machine.Launch].
func NewMachine[S State[T], T any](g *graph.Graph[S, reflect.Type], v T, opts ...Option[S]) *Machine[S, T] {
	m := &Machine[S, T]{
		graph: g,
		value: v,
	}
	for _, opt := range opts {
		opt(&m.config)
	}
	return m
}

// Machine represents a finite state machine that manages states of type S.
// It provides lifecycle management (Launch, Stop), event handling (Trigger),
// and state inspection (CurrentState).
//
// Trigger and Stop return an error while a transition is in progress (from Entry
// or an exit action); a timer callback runs between transitions and may call
// them. Launch returns an error from inside any callback. A panic from a
// callback propagates to whoever ran it (the caller of Launch, Trigger or Stop,
// or the dispatcher of a timer callback); the machine can still be stopped
// afterward.
//
// IMPORTANT: Machine is NOT safe for concurrent access from multiple goroutines.
// To safely access the machine from multiple goroutines, use the Dispatcher.AfterFunc:
//
//	go func() {
//	    dispatcher.AfterFunc(0, func() {
//	        // Safe access to machine methods from another goroutine
//	        machine.Trigger(event)
//	    })
//	}()
//
// This leverages the Dispatcher's internal synchronization mechanism for safe concurrent access.
type Machine[S State[T], T any] struct {
	graph  *graph.Graph[S, reflect.Type]
	value  T
	config machineConfig[S]

	// node is the graph node the machine is at. It is read only while the
	// manager holds a visit, so it keeps the last node after a stop.
	node *graph.Node[S, reflect.Type]
	// manager holds the current visit and its timers: Set ends the visit with
	// its timers, so the machine is running exactly while it holds one.
	manager Manager[*stateVisit[T]]
	// callback is the callback the machine is running, if any. With the visit it
	// decides which of the machine's methods the callback may call.
	callback callbackKind
}

// Value returns the value the machine carries.
func (m *Machine[S, T]) Value() T {
	return m.value
}

// Launch enters the initial state and processes the events the Entry chain
// returns until one stays. It returns an error for a nil graph or one
// without an initial node, a running machine or a call from inside a callback,
// and otherwise the first failure of the chain as [Machine.Trigger] does,
// leaving the machine where Trigger would.
func (m *Machine[S, T]) Launch() error {
	if m.graph == nil || m.graph.InitialNode == nil {
		return errNilGraph
	}
	switch {
	case m.callback != callbackKindNone:
		return errInCallback
	case m.manager.Get() != nil:
		return errAlreadyLaunched
	}
	defer m.popCallback(m.pushCallback(callbackKindTransition))
	return m.run(m.graph.InitialNode, nil)
}

// Trigger performs the transition for event and processes the events the Entry
// chain returns. It returns the first failure: a machine that is not running, a
// transition in progress, a nil event, an event with no transition, the *Guarded
// of an exit action (returned as is, not wrapped), or a Command this package did not
// construct. A failure after the first transition leaves the machine in the state
// whose Entry returned the failing Command.
//
// A *Guarded returned after the first transition blocks an event that an Entry
// returned, not event itself: the requested transition has happened, and
// [Machine.CurrentState] reports the state that kept its exit action.
func (m *Machine[S, T]) Trigger(event Event) error {
	v := m.manager.Get()
	switch {
	case v == nil:
		return errNotLaunched
	case m.callback == callbackKindTransition:
		return errInTransition
	}
	defer m.popCallback(m.pushCallback(callbackKindTransition))
	next, err := m.leave(v, event)
	if err != nil {
		return err
	}
	return m.run(next, event)
}

// Stop stops the machine: the exit action runs with a nil event and cannot block
// the stop (its *Guarded is reported to the [Tracer]), then the visit's timers are
// canceled and its handles invalidated. It returns an error when the machine is
// not running or a transition is in progress; Entry stops the machine by
// returning [Stop].
func (m *Machine[S, T]) Stop() error {
	v := m.manager.Get()
	switch {
	case v == nil:
		return errNotLaunched
	case m.callback == callbackKindTransition:
		return errInTransition
	}
	defer m.popCallback(m.pushCallback(callbackKindTransition))
	m.stop(v)
	return nil
}

// CurrentState returns the state of the current visit, or an error when the
// machine is not running. Inside a callback it is the state the callback belongs
// to until the callback itself moves the machine; for an exit action, the state
// being left.
func (m *Machine[S, T]) CurrentState() (S, error) {
	if m.manager.Get() == nil {
		var zero S
		return zero, errNotLaunched
	}
	return m.node.State, nil
}

// run enters node with event and keeps entering the destinations of the events
// the Entry methods return, until one stays or stops. The chain is a loop so that
// its length does not grow the stack.
func (m *Machine[S, T]) run(node *graph.Node[S, reflect.Type], event Event) error {
	for {
		v, next := m.enter(node, event)
		switch next := next.(type) {
		case nil:
			return nil
		case stopCommand:
			m.stop(v)
			return nil
		case triggerCommand:
			destination, err := m.leave(v, next.event)
			if err != nil {
				return err
			}
			node, event = destination, next.event
		default:
			// A type embedding Command bypasses the sealed method; report it
			// rather than enter the same node again.
			return fmt.Errorf("%w: %T", errUnknownCommand, next)
		}
	}
}

// leave finds the destination of event from visit v and runs its exit action. A
// nil event or one with no transition is an error before the exit action runs; a
// blocked transition returns the *Guarded and keeps the visit with its exit
// action and timers.
func (m *Machine[S, T]) leave(v *stateVisit[T], event Event) (*graph.Node[S, reflect.Type], error) {
	if event == nil {
		return nil, errNilEvent
	}
	next, found := m.graph.FindNext(m.node, reflect.TypeOf(event))
	if !found {
		return nil, fmt.Errorf("%w: %T from %v", errNoTransition, event, m.node.State)
	}
	if guarded := v.runExit(event); guarded != nil {
		return nil, guarded
	}
	return next, nil
}

// enter ends the current visit, if any, and starts one of node, returning it and
// what its Entry asks for next. The Tracer is notified inside the transition, so
// it cannot start another one.
func (m *Machine[S, T]) enter(node *graph.Node[S, reflect.Type], event Event) (*stateVisit[T], Command) {
	var from S
	if m.manager.Get() != nil {
		from = m.node.State
	}
	v := &stateVisit[T]{machine: m}
	m.node = node
	m.manager.Set(v)

	if m.config.tracer != nil {
		m.config.tracer.Trace(Transition[S]{From: from, To: node.State, Event: event})
	}
	return v, node.State.Entry((*EntryMachine[T])(v), event)
}

// stop ends the current visit and the run of the machine, reporting to the
// [Tracer] the *Guarded the exit action returned in vain. The Tracer is notified
// inside the stop, so it cannot relaunch the machine from there.
func (m *Machine[S, T]) stop(v *stateVisit[T]) {
	guarded := v.runExit(nil)

	from := m.node.State
	m.manager.Set(nil)
	if m.config.tracer != nil {
		var zero S
		m.config.tracer.Trace(Transition[S]{From: from, To: zero, Guarded: guarded})
	}
}

// pushCallback makes k the callback the machine is running and returns the one
// it replaced, to be given to popCallback in a defer. Restoring instead of
// clearing keeps it consistent when a transition is started from a timer
// callback, and when a panic unwinds.
func (m *Machine[S, T]) pushCallback(k callbackKind) callbackKind {
	previous := m.callback
	m.callback = k
	return previous
}

// popCallback restores the callback pushCallback replaced.
func (m *Machine[S, T]) popCallback(previous callbackKind) {
	m.callback = previous
}

// currentVisit returns the visit the machine is in, or nil when it is not
// running.
func (m *Machine[S, T]) currentVisit() *stateVisit[T] {
	return m.manager.Get()
}

// afterFunc schedules f as a timer callback of the current visit. A dispatcher
// that fires synchronously is unsupported: f would run as a timer callback inside
// the callback that registers it, and the fired timer would stay held by the
// manager.
func (m *Machine[S, T]) afterFunc(d task.Dispatcher, delay time.Duration, f func()) {
	m.manager.AfterFunc(d, delay, func() {
		defer m.popCallback(m.pushCallback(callbackKindTimer))
		f()
	})
}

// callbackKind is what the machine is running: [Machine.Launch], [Machine.Trigger]
// and [Machine.Stop] hold it for the whole call they make, so every callback they
// reach runs in a transition and cannot start another one, and a timer callback
// runs between transitions and can.
type callbackKind int

const (
	callbackKindNone callbackKind = iota
	callbackKindTimer
	callbackKindTransition
)

var (
	errStateLeft            = errors.New("state has already been left")
	errExitActionRegistered = errors.New("exit action already registered")
)

// stateVisit is one visit of a state as its callbacks see it: the machine's
// Manager holds the current one, so the registrations and the timers of a visit
// end together. It carries the value type only, so [EntryMachine] and
// [AfterFuncMachine] are named types of it and the state type stays out of their
// signatures; the machine is reached through machineOps.
type stateVisit[T any] struct {
	machine machineOps[T]
	// The exit action registered by OnExit
	exit func(event Event) *Guarded
}

// machineOps is the machine as the handles use it.
type machineOps[T any] interface {
	Value() T
	currentVisit() *stateVisit[T]
	afterFunc(d task.Dispatcher, delay time.Duration, f func())
	Trigger(event Event) error
	Stop() error
}

// runExit runs the exit action of the visit, if any. It stays registered while it
// runs, so it cannot register a second one from there; a panic consumes it, so a
// later Stop does not run it again.
func (v *stateVisit[T]) runExit(event Event) *Guarded {
	exit := v.exit
	if exit == nil {
		return nil
	}
	returned := false
	defer func() {
		if !returned {
			v.exit = nil
		}
	}()
	guarded := exit(event)
	returned = true
	return guarded
}

// stale reports whether the machine has left the visit, which then takes no
// further registration.
func (v *stateVisit[T]) stale() bool {
	return v.machine.currentVisit() != v
}

func (v *stateVisit[T]) afterFunc(d task.Dispatcher, delay time.Duration, f func(m *AfterFuncMachine[T])) error {
	if v.stale() {
		return errStateLeft
	}
	v.machine.afterFunc(d, delay, func() {
		f((*AfterFuncMachine[T])(v))
	})
	return nil
}

// EntryMachine is the machine as seen from Entry, which runs inside a transition
// and cannot start another one. OnExit and AfterFunc return an error and register
// nothing once the machine has left the state; until then they are valid inside
// the exit action of the visit as well.
type EntryMachine[T any] stateVisit[T]

// Value returns the value the machine carries.
func (m *EntryMachine[T]) Value() T {
	return (*stateVisit[T])(m).machine.Value()
}

// OnExit registers the exit action of this visit, called once when the machine
// leaves the state with the causing event, or nil on a stop. A *Guarded blocks
// the transition and keeps the exit action for the next event; a stop cannot be
// blocked. A second call returns an error and keeps the first.
//
// A panic in the exit action leaves the machine in the state with the timers of
// the visit scheduled, and consumes the exit action: a later Stop or transition
// runs without it.
func (m *EntryMachine[T]) OnExit(f func(event Event) *Guarded) error {
	v := (*stateVisit[T])(m)
	if v.stale() {
		return errStateLeft
	}
	if v.exit != nil {
		return errExitActionRegistered
	}
	v.exit = f
	return nil
}

// AfterFunc schedules f on the dispatcher after delay with the [AfterFuncMachine]
// of this visit. The timer is canceled when the machine leaves the state; the
// dispatcher is not owned by the machine. A timer scheduled from inside the exit
// action is canceled with the visit when the transition proceeds and stays when
// the exit action blocks it. A dispatcher that runs f synchronously inside its
// AfterFunc is not supported.
func (m *EntryMachine[T]) AfterFunc(d task.Dispatcher, delay time.Duration, f func(m *AfterFuncMachine[T])) error {
	return (*stateVisit[T])(m).afterFunc(d, delay, f)
}

// AfterFuncMachine is the machine as seen from a timer callback, which runs
// between transitions. Trigger, Stop and AfterFunc return an error once the
// machine has left the state, including through the handle's own Trigger or Stop.
type AfterFuncMachine[T any] stateVisit[T]

// Value returns the value the machine carries.
func (m *AfterFuncMachine[T]) Value() T {
	return (*stateVisit[T])(m).machine.Value()
}

// Trigger performs the transition for event as [Machine.Trigger] does. The handle
// is stale once the state is left, which is also the case when a later event of
// the chain fails; a failure before the first transition keeps it valid.
func (m *AfterFuncMachine[T]) Trigger(event Event) error {
	v := (*stateVisit[T])(m)
	if v.stale() {
		return errStateLeft
	}
	return v.machine.Trigger(event)
}

// Stop stops the machine as [Machine.Stop] does.
func (m *AfterFuncMachine[T]) Stop() error {
	v := (*stateVisit[T])(m)
	if v.stale() {
		return errStateLeft
	}
	return v.machine.Stop()
}

// AfterFunc schedules another timer of this visit; see [EntryMachine.AfterFunc].
func (m *AfterFuncMachine[T]) AfterFunc(d task.Dispatcher, delay time.Duration, f func(m *AfterFuncMachine[T])) error {
	return (*stateVisit[T])(m).afterFunc(d, delay, f)
}
