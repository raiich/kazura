package state

import (
	"fmt"
	"reflect"
	"time"

	"github.com/raiich/kazura/state/graph"
)

var errMethodNotCallable = fmt.Errorf("method is not callable here")

// NewMachine creates a new state machine with the given graph and value.
// Returns a configured but not yet launched machine.
func NewMachine[S State[T], T any](g *graph.Graph[S, reflect.Type], v T) *Machine[S, T] {
	if g == nil {
		panic("graph cannot be nil")
	}

	m := &Machine[S, T]{
		graph: g,
		value: v,
	}
	m.accessor.machine = m
	return m
}

// Machine represents a finite state machine that manages states of type S.
// It provides lifecycle management (Launch, Stop), event handling (Trigger),
// and state inspection (CurrentState).
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
	graph      *graph.Graph[S, reflect.Type]
	value      T
	manager    Manager[*graph.Node[S, reflect.Type]]
	state      machineState
	context    executionContext
	onExit     func(machine *ExitMachine[T], event Event) *Guarded
	afterEntry func(machine *AfterEntryMachine[T])
	accessor   machineAccessor[T]
}

func (m *Machine[S, T]) Value() T {
	return m.value
}

// Launch starts the state machine and transitions to the initial state.
// Returns an error if the machine is already launched.
func (m *Machine[S, T]) Launch() error {
	if m.state == stateLaunched {
		return fmt.Errorf("machine is already launched")
	}
	switch m.context {
	case executionContextNone:
		// ok
	default:
		return errMethodNotCallable
	}

	defer func() {
		m.context = executionContextNone
	}()

	m.state = stateLaunched
	nextNode := m.graph.InitialNode

	// Enter the initial state
	m.manager.Set(nextNode)
	m.context = executionContextEntry
	nextNode.State.Entry((*EntryMachine[T])(&m.accessor), nil)

	// Execute pending after-entry callbacks
	for {
		callback, ok := m.popAfterEntry()
		if !ok {
			break
		}
		m.context = executionContextAfterEntry
		callback((*AfterEntryMachine[T])(&m.accessor))
	}

	return nil
}

// Trigger processes an event and potentially transitions to a new state.
// Returns an error if the event is not valid for the current state.
func (m *Machine[S, T]) Trigger(event Event) error {
	switch m.context {
	case executionContextNone:
		// ok
	default:
		return errMethodNotCallable
	}

	defer func() {
		m.context = executionContextNone
	}()
	return m.trigger(event)
}

func (m *Machine[S, T]) triggerFromAfterFunc(event Event) error {
	switch m.context {
	case executionContextAfterFunc:
		// ok
	default:
		return errMethodNotCallable
	}
	return m.trigger(event)
}

func (m *Machine[S, T]) triggerFromAfterEntry(event Event) error {
	switch m.context {
	case executionContextAfterEntry:
		// ok
	default:
		return errMethodNotCallable
	}
	return m.triggerOnce(event)
}

func (m *Machine[S, T]) trigger(event Event) error {
	// Perform the transition
	err := m.triggerOnce(event)
	if err != nil {
		return err
	}

	// Execute pending after-entry callbacks
	for {
		callback, ok := m.popAfterEntry()
		if !ok {
			break
		}
		m.context = executionContextAfterEntry
		callback((*AfterEntryMachine[T])(&m.accessor))
	}

	return nil
}

// triggerOnce performs a single state transition without executing after-entry callbacks.
func (m *Machine[S, T]) triggerOnce(event Event) error {
	if event == nil {
		return fmt.Errorf("event cannot be nil")
	}
	if m.state != stateLaunched {
		return fmt.Errorf("machine is not launched")
	}

	currentNode := m.manager.Get()
	eventType := reflect.TypeOf(event)

	// Find transition for the event
	nextNode, found := currentNode.FindNext(eventType)
	if !found {
		// Try wildcard transitions
		nextNode, found = m.graph.Wildcards.FindNext(eventType)
		if !found {
			return fmt.Errorf("no transition found for event %T from state %v", event, currentNode.State)
		}
	}

	// Execute exit callback if present
	if callback, ok := m.popExitAction(); ok {
		m.context = executionContextExit
		guarded := callback((*ExitMachine[T])(&m.accessor), event)
		if guarded != nil {
			return guarded
		}
		// Check if machine was stopped during exit callback
		if m.state == stateStopped {
			return nil
		}
	}

	// Transition to the new state
	m.manager.Set(nextNode)
	m.context = executionContextEntry
	nextNode.State.Entry((*EntryMachine[T])(&m.accessor), event)

	return nil
}

// Stop shuts down the state machine and cancels all pending timers.
// Returns an error if the machine is already stopped.
func (m *Machine[S, T]) Stop() error {
	if m.state == stateStopped {
		return fmt.Errorf("machine is already stopped")
	}

	m.state = stateStopped
	// Clear state and cancel all timers
	m.manager.Set(nil)
	// Clear any pending callbacks
	m.onExit = nil
	m.afterEntry = nil

	return nil
}

// CurrentState returns the current state of the machine.
// Returns an error if the machine has not been launched or has been stopped.
func (m *Machine[S, T]) CurrentState() (S, error) {
	if m.state != stateLaunched {
		var zero S
		return zero, fmt.Errorf("machine is not launched")
	}

	currentNode := m.manager.Get()
	if currentNode == nil {
		var zero S
		return zero, fmt.Errorf("no current state")
	}

	return currentNode.State, nil
}

func (m *Machine[S, T]) doAfterFunc(dispatcher Dispatcher, d time.Duration, callback func(machine *AfterFuncMachine[T])) {
	switch m.context {
	default:
		// ok
	}
	m.manager.AfterFunc(dispatcher, d, func() {
		m.context = executionContextAfterFunc
		defer func() {
			m.context = executionContextNone
		}()
		callback((*AfterFuncMachine[T])(&m.accessor))
	})
}

func (m *Machine[S, T]) doAfterEntry(callback func(machine *AfterEntryMachine[T])) error {
	switch m.context {
	case executionContextEntry:
		// ok
	default:
		return fmt.Errorf("AfterEntry is not callable here")
	}

	if m.afterEntry != nil {
		return fmt.Errorf("callback for AfterEntry already registered")
	}
	m.afterEntry = callback
	return nil
}

// popAfterEntry pops and returns the current after-entry callback, if any.
// Returns the callback function and true if one was available, or nil and false if none.
func (m *Machine[S, T]) popAfterEntry() (func(machine *AfterEntryMachine[T]), bool) {
	if m.afterEntry == nil {
		return nil, false
	}
	callback := m.afterEntry
	m.afterEntry = nil
	return callback, true
}

func (m *Machine[S, T]) doOnExit(callback func(machine *ExitMachine[T], event Event) *Guarded) error {
	switch m.context {
	case executionContextExit:
		return errMethodNotCallable
	default:
		// ok
	}

	if m.onExit != nil {
		return fmt.Errorf("exit callback already registered")
	}
	m.onExit = callback
	return nil
}

// popExitAction pops and returns the current exit callback, if any.
// Returns the callback function and true if one was available, or nil and false if none.
func (m *Machine[S, T]) popExitAction() (func(machine *ExitMachine[T], event Event) *Guarded, bool) {
	if m.onExit == nil {
		return nil, false
	}
	callback := m.onExit
	m.onExit = nil
	return callback, true
}

// machineState represents the current lifecycle state of the machine
type machineState int

const (
	stateStopped machineState = iota
	stateLaunched
)

// executionContext represents the current execution context of the machine.
type executionContext int

const (
	executionContextNone executionContext = iota
	executionContextEntry
	executionContextAfterEntry
	executionContextAfterFunc
	executionContextExit
)

// EntryMachine provides operations available when entering a state.
type EntryMachine[T any] machineAccessor[T]

// Value returns the current value stored in the machine.
func (m *EntryMachine[T]) Value() T {
	return m.machine.Value()
}

// AfterFunc schedules a callback to be executed after the specified duration.
// The callback will be canceled if the state changes before the timer fires.
// The dispatcher parameter is used to schedule the timer execution.
func (m *EntryMachine[T]) AfterFunc(dispatcher Dispatcher, d time.Duration, callback func(machine *AfterFuncMachine[T])) {
	m.machine.doAfterFunc(dispatcher, d, callback)
}

// AfterEntry schedules a callback to be executed immediately after the Entry method completes.
// Returns an error if the callback registration fails.
func (m *EntryMachine[T]) AfterEntry(callback func(machine *AfterEntryMachine[T])) error {
	return m.machine.doAfterEntry(callback)
}

// OnExit registers a callback to be executed when leaving this state (exit-action).
// The callback can return a Guarded error to prevent the state transition.
// Only one exit-action can be registered per state.
// Returns an error if the callback registration fails.
func (m *EntryMachine[T]) OnExit(callback func(machine *ExitMachine[T], event Event) *Guarded) error {
	return m.machine.doOnExit(callback)
}

// AfterFuncMachine provides operations available within AfterFunc callbacks.
type AfterFuncMachine[T any] machineAccessor[T]

// Value returns the current data value stored in the state machine.
func (m *AfterFuncMachine[T]) Value() T {
	return m.machine.Value()
}

// AfterFunc schedules another callback to be executed after the specified duration.
// The dispatcher parameter is used to schedule the timer execution.
func (m *AfterFuncMachine[T]) AfterFunc(dispatcher Dispatcher, d time.Duration, callback func(machine *AfterFuncMachine[T])) {
	m.machine.doAfterFunc(dispatcher, d, callback)
}

// Trigger processes an event and transitions to a new state.
// Returns an error if the event is not valid for the current state.
func (m *AfterFuncMachine[T]) Trigger(event Event) error {
	return m.machine.triggerFromAfterFunc(event)
}

// AfterEntryMachine provides operations available within AfterEntry callbacks.
type AfterEntryMachine[T any] machineAccessor[T]

// Value returns the current value stored in the machine.
func (m *AfterEntryMachine[T]) Value() T {
	return m.machine.Value()
}

// Trigger processes an event and transitions to a new state.
// Returns an error if the event is not valid for the current state.
func (m *AfterEntryMachine[T]) Trigger(event Event) error {
	return m.machine.triggerFromAfterEntry(event)
}

// ExitMachine provides operations available within exit callbacks.
type ExitMachine[T any] machineAccessor[T]

// Value returns the current data value stored in the state machine.
func (m *ExitMachine[T]) Value() T {
	return m.machine.Value()
}

type stateMachine[T any] interface {
	Value() T
	doAfterFunc(dispatcher Dispatcher, d time.Duration, callback func(machine *AfterFuncMachine[T]))
	doAfterEntry(callback func(machine *AfterEntryMachine[T])) error
	triggerFromAfterFunc(event Event) error
	triggerFromAfterEntry(event Event) error
	doOnExit(callback func(machine *ExitMachine[T], event Event) *Guarded) error
}

type machineAccessor[T any] struct {
	machine stateMachine[T]
}
