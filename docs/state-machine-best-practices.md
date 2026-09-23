# kazura state.Machine Best Practices

This document summarizes best practices for effectively utilizing kazura's state.Machine.

## Overview

### What is state.Machine?

`state.Machine` is a Go library for implementing Finite State Machines (FSM). By explicitly managing state transitions,
it organizes complex state logic and achieves highly maintainable code.

### Key Benefits

- **Explicit State Transitions**: Defining state transitions as a graph structure enables visualization of system behavior
- **Type Safety**: Leverages Go generics for compile-time type checking
- **Testability**: Dispatcher abstraction makes time-dependent processing testable
- **Concurrency**: A Dispatcher serializes the machine's timers with calls from other goroutines

## Basic Usage

### Defining Type Aliases

Define project-specific type aliases to make the state machine easier to use.

```go
package main

import (
    "github.com/raiich/kazura/state"
    "github.com/raiich/kazura/task"
)

// Data type definition
type VendingMachine struct {
    Coins      int
    Dispatcher task.Dispatcher
}

// Type aliases (omit verbose type parameters)
type State = state.State[*VendingMachine]
type Event = state.Event

type EntryMachine = state.EntryMachine[*VendingMachine]
type AfterFuncMachine = state.AfterFuncMachine[*VendingMachine]
type Transition = state.Transition[State]

// On helper function (simplifies state transition definitions)
func On[E Event](from, to State) state.Edge[State] {
    return state.On[State, E](from, to)
}
```

**Key Points**:
- Pointer types (`*VendingMachine`) are recommended for data (to share data)
- Defining type aliases improves code readability
- The `On` helper function simplifies state transition definitions

### Creating a State Graph

Define state transitions as a graph.

```go
var stateGraph = must.Must(state.NewGraph[State](
    InitialState{},                                   // Initial state
    On[CoinEvent](InitialState{}, WaitingState{}),    // Coin insertion → waiting state
    On[CoinEvent](WaitingState{}, WaitingState{}),    // Additional coin during waiting
    On[*ButtonEvent](WaitingState{}, PouringState{}), // Button press → pouring state
    On[DoneEvent](PouringState{}, InitialState{}),    // Completion → initial state
    On[DoneEvent](WaitingState{}, InitialState{}),    // Cancel → initial state
))
```

**Key Points**:
- First argument is the initial state
- Use `On[EventType](from, to)` to define state transitions
- Self-transitions (loops) to the same state are possible
- `must.Must()` simplifies error handling (graph structure errors cause panic)

### Machine Initialization and Lifecycle

```go
func main() {
    // Create dispatcher (time management)
    dispatcher := eventloop.NewDispatcher(time.Now())

    // Initialize data
    vendingMachine := VendingMachine{
        Dispatcher: dispatcher,
    }

    // Create and launch machine
    machine := state.NewMachine(stateGraph, &vendingMachine)
    must.NoError(machine.Launch())

    // Trigger events
    must.NoError(machine.Trigger(CoinEvent(1)))
    must.NoError(machine.Trigger(&ButtonEvent{Item: "water"}))

    // Stop machine (if needed)
    must.NoError(machine.Stop())
}
```

**Lifecycle**:
1. `NewMachine()` - Create machine (not yet launched)
2. `Launch()` - Transition to initial state, execute Entry
3. `Trigger()` - State transitions via events
4. `Stop()` - Cancel all timers and stop

## State Implementation

### Basic State Definition

All states must implement the `State` interface.

```go
type InitialState struct{}

func (s InitialState) Entry(machine *EntryMachine, event Event) state.Command {
    // Processing when entering the state
    if event != nil {
        log.Info("enter InitialState", "event", event)
    }

    // Data initialization
    machine.Value().Coins = 0
    return nil
}
```

**Key Points**:
- Access data via `machine.Value()`
- What `Entry` receives and what its return value means is the doc of `state.State`

### State Interface Customization (Optional)

The standard kazura `State` interface can be extended by adding application-specific methods.

When integrating with game engines, add per-frame processing methods.

```go
type State interface {
    state.State[*Data]  // Entry method
    HandleInput(sc *Scene, input ui.Input)
    Draw(sc *Scene, screen *ebiten.Image)
}

type RunningState struct{}

func (s RunningState) Entry(machine *state.EntryMachine[*Data], event state.Event) state.Command {
    // State initialization
    return nil
}

func (s RunningState) HandleInput(sc *Scene, input ui.Input) {
    // Input processing
}

func (s RunningState) Draw(sc *Scene, screen *ebiten.Image) {
    // Drawing processing
}
```

**Important Notes**:
- Custom methods must be implemented for **all states**
- The standard `Entry` method is required

## Event Design

### Basic Event Definitions

Events can be defined using any type.

```go
// Simple event (no data)
type CoinEvent int

// Struct event (with data)
type ButtonEvent struct {
    Item string
}

// String event
type DoneEvent string
```

**Usage**:
- No data needed: Empty struct `struct{}` or int/string
- Data needed: Use struct
- Pointer type events: When distinction is needed in state transition graph (`*ButtonEvent` vs `ButtonEvent`)

### Triggering Events

```go
// Value type events
must.NoError(machine.Trigger(CoinEvent(1)))
must.NoError(machine.Trigger(DoneEvent("timeout")))

// Pointer type events
must.NoError(machine.Trigger(&ButtonEvent{Item: "coffee"}))
```

### Wildcard Transitions

By setting `from` to `nil`, you can define transitions from any state.

```go
var stateGraph = must.Must(state.NewGraph[State](
    InitialState{},
    On[QuitEvent](nil, InitialState{}), // Quit → initial state from any state
    // ...
))
```

**Use Cases**:
- Error handling (from any state to error state)
- Reset functionality (from any state to initial state)
- Global event processing

## Timers and Asynchronous Processing

### Delayed Execution with AfterFunc

Use timers to automatically trigger events after a specified duration.

```go
func (s WaitingState) Entry(machine *EntryMachine, event Event) state.Command {
    vendingMachine := machine.Value()

    // Trigger timeout event after 10 seconds
    must.NoError(machine.AfterFunc(vendingMachine.Dispatcher, 10*time.Second, func(machine *AfterFuncMachine) {
        must.NoError(machine.Trigger(DoneEvent("timeout")))
    }))
    return nil
}
```

**Features**:
- The timer belongs to the visit; cancellation and what the callback may call are the docs of `EntryMachine.AfterFunc` and `AfterFuncMachine`
- The callback runs on the `Dispatcher`, serialized with everything else submitted to it
- In tests, you can advance time with `eventloop.Dispatcher.FastForward()`

### Immediate Post-Processing with the Entry Return Value

Return `state.Trigger(event)` to have the machine process the event once `Entry` returns, or `nil` to stay.

```go
func (s PouringState) Entry(machine *EntryMachine, event state.Event) state.Command {
    log.Info("pouring", "item", event.(*ButtonEvent).Item)

    // The machine performs this transition once Entry returns
    return state.Trigger(DoneEvent("done"))
}
```

**Use Cases**:
- When you want to transition to the next state immediately after Entry initialization
- `Entry` cannot call `Trigger` or `Stop` itself (`state.Machine` doc); the returned `state.Command` is how a state moves on or stops

#### Execution Order in Chained Transitions

Transitions never nest: the machine performs the transition an `Entry` returns after that `Entry` has returned.

Example: State A → State B → State C, where State B's Entry returns the Trigger for the second transition:

```
1. Trigger(event1)
2.   State A: exit action → Trace → State B: Entry — returns Trigger(event2)
3.   State B: exit action → Trace → State C: Entry — returns nil
4. Trigger returns
```

**Key Point**: What the caller gets when a chained event fails is the doc of `Machine.Trigger`.

### Dispatcher Selection

Choose a Dispatcher based on your state machine use case.

```go
// When you want to control time (games, animation, tests)
dispatcher := eventloop.NewDispatcher(time.Now())
must.NoError(dispatcher.FastForward(time.Now())) // Call every frame

// For real-time processing
dispatcher := mutex.NewDispatcher()
// or
dispatcher := queue.NewDispatcher()
go dispatcher.Serve(ctx)

// To suspend the pending timers (e.g. while a game is paused)
dispatcher := pausable.NewDispatcher(mutex.NewDispatcher(), time.Now)
must.NoError(dispatcher.Pause())
must.NoError(dispatcher.Resume())
```

**Selection Criteria**:
- **eventloop**: When you have a periodic update loop (like games) and want manual time control (advance time with `FastForward()`). Also useful for tests.
- **mutex/queue**: For real-time processing
- **pausable**: Wraps another dispatcher to suspend and resume its pending timers

**Dispatcher Feature Comparison**:

| Dispatcher | Time Control            | Execution Method              | Use Case                 |
|------------|-------------------------|-------------------------------|--------------------------|
| eventloop  | Manual (FastForward)    | Caller goroutine              | Game loops, tests        |
| queue      | Real-time (time.AfterFunc) | Separate goroutine (Serve) | Web servers, workers     |
| mutex      | Real-time (time.AfterFunc) | InvokeFunc: caller goroutine; timers: timer goroutine | Simple servers |
| pausable   | The base's, plus Pause / Resume | That of the base dispatcher | Games with a pause screen |

### Calling the Machine from Other Goroutines

Calls from other goroutines go through the `Dispatcher` that runs the machine's timers:

```go
dispatcher.InvokeFunc(func() {
    must.NoError(machine.Trigger(event))
})
```

Inside a callback (`Entry`, an exit action or a timer callback) do not call `InvokeFunc` or `Task.Wait`; either may deadlock, depending on the dispatcher. To act on the machine after the callback, use `machine.AfterFunc(dispatcher, 0, ...)`.

## Guard Conditions

### Guards with OnExit

Perform validation before exiting a state and block transitions if conditions are not met.

```go
func (s WaitingState) Entry(machine *EntryMachine, event Event) state.Command {
    vendingMachine := machine.Value()

    // Validation when exiting state
    must.NoError(machine.OnExit(func(event Event) *state.Guarded {
        switch e := event.(type) {
        case CoinEvent:
            return nil // Always allow coin insertion
        case *ButtonEvent:
            // Coffee requires 2 coins
            if e.Item == "coffee" && vendingMachine.Coins < 2 {
                return Guarded("2 coin(s) for %v, but %d", e.Item, vendingMachine.Coins)
            }
        }
        return nil
    }))
    return nil
}

// Helper function
func Guarded(format string, args ...any) *state.Guarded {
    return &state.Guarded{
        Reason: fmt.Errorf(format, args...),
    }
}
```

**Usage Example**:
```go
// Successful transition
must.NoError(machine.Trigger(CoinEvent(1)))
must.NoError(machine.Trigger(CoinEvent(2)))
must.NoError(machine.Trigger(&ButtonEvent{Item: "coffee"}))

// Failed transition (returns error)
must.NoError(machine.Trigger(CoinEvent(1)))
err := machine.Trigger(&ButtonEvent{Item: "coffee"})
// err: "2 coin(s) for coffee, but 1"
```

**Key Points**:
- `OnExit` is registered in each Entry and executed when exiting that state
- What a `*state.Guarded` does is the doc of `EntryMachine.OnExit`; how the caller receives it, that of `Machine.Trigger`

## Observability

### Tracing State Transitions

Use `state.WithTracer` to observe every state transition from outside the machine.

```go
type Tracer[S any] interface {
    Trace(t Transition[S])
}

type Transition[S any] struct {
    From, To S
    Event    Event
    Guarded  *Guarded
}
```

Pass a `Tracer` implementation when constructing the machine. `%T` prints the
state type name, which is usually more informative than `%v` for logging
(states with no logging-relevant fields print as `{}` or a bare address
under `%v`).

```go
import (
    "fmt"
    "log/slog"
)

type transitionLogger struct{}

func (transitionLogger) Trace(t Transition) {
    from, to, event := t.From, t.To, t.Event
    slog.Info("transition",
        "from", fmt.Sprintf("%T", from),
        "to", fmt.Sprintf("%T", to),
        "event", event)
}

machine := state.NewMachine(stateGraph, &data, state.WithTracer[State](transitionLogger{}))
```

`State` here is the project's type alias for `state.State[*Data]` and
`Transition` for `state.Transition[State]`. The explicit `[State]` on
`WithTracer` is required because Go cannot infer `S` from the receiver type of
`transitionLogger.Trace` alone.

**Call Semantics**: when `Trace` is called and what `Transition` carries on `Launch` and `Stop` are the docs of `state.Tracer` and `state.Transition`.

**Use Cases**:
- Structured transition logging without cluttering `Entry` methods
- Generating debug traces or timelines for analysis
- Collecting transition metrics (e.g., transition counts per state pair)

See [examples/vending-machine](../examples/vending-machine/main.go) for a working example.

## Architecture Patterns

### Gateway Struct Pattern

When utilizing state machines, we recommend the pattern of **having a gateway struct with `*Data` (embedded) and `machine` (private)**.

```go
type Scene struct {
    *Data                              // Embedded for direct access
    machine *state.Machine[State, *Data]  // Private to hide details
}

func NewScene() *Scene {
    data := &Data{...}
    machine := state.NewMachine(stateGraph, data)
    must.NoError(machine.Launch())
    return &Scene{Data: data, machine: machine}
}
```

**Benefits**:
- **Encapsulation**: Hide state machine details from external access
- **Concise Access**: `s.GameData.Laps` (without embedding: `s.Data.GameData.Laps`)
- **Controlled Operations**: Operate state machine only through public methods

**Difference from vending-machine**: vending-machine manages directly for educational simplicity, but for actual applications, the gateway struct pattern is recommended.

### Nested State Machines

For complex systems, arrange multiple state machines hierarchically.

```go
// Scene layer definition
type Scene struct {
    *Data
    machine *state.Machine[State, *Data]
}

type Data struct {
    Character *character.Character  // Character layer definition
    GameData  GameData
}
```

```go
// Character layer definition
// package character
type Character struct {
    *Data
    machine *state.Machine[State, *Data]
}

type Data struct {
    X int
    Y int
}
```

**State Independence**:
- Scene state machine: Title, Countdown, Running, Result
- Character state machine: Stopped, Running (animation control)

**Coordination**:
```go
func (s RunningState) Entry(machine *EntryMachine, event Event) state.Command {
    data := machine.Value()

    // Launch Character state machine
    must.NoError(data.Character.Ready())
    return nil
}
```

### Multiple State Machine Coordination

Each state machine operates independently while coordinating through method calls.

```go
// Scene → Character
func (s RunningState) HandleInput(sc *Scene, input ui.Input) {
    sc.Character.Kick()  // Call Character method
}
```

```go
// Character public methods
func (c *Character) Kick() {
    c.Speed = c.Config.SpeedMax
}

func (c *Character) Ready() error {
    return c.machine.Trigger(startEvent{})  // Internal state machine event
}

func (c *Character) Stop() error {
    return c.machine.Trigger(stopEvent{})
}
```

**Key Points**:
- Hide state machine details from external access (encapsulation)
- Communicate between state machines through public methods
- Each state machine can be tested independently

## Implementation Examples

### Example 1: Vending Machine (vending-machine)

**State Transition Diagram**:
```mermaid
stateDiagram-v2
  [*] --> InitialState
  InitialState --> WaitingState: CoinEvent
  PouringState --> InitialState: DoneEvent
  WaitingState --> InitialState: DoneEvent
  WaitingState --> PouringState: ButtonEvent
  WaitingState --> WaitingState: CoinEvent
```

**Features**:
- Guard conditions: Block purchase with insufficient funds
- Timeout: Auto-reset after 10 seconds of inactivity (DoneEvent)
- Self-transition: Additional coins in WaitingState (CoinEvent)

### Example 2: Game

**Scene State Machine**:

```mermaid
stateDiagram-v2
  [*] --> TitleState
  CountdownState --> RunningState: CountdownCompleteEvent
  ResultState --> TitleState: ResultTimeoutEvent
  RunningState --> ResultState: GameEndEvent
  TitleState --> CountdownState: StartGameEvent
```

**Character State Machine (Animation)**:

```mermaid
stateDiagram-v2
  [*] --> stoppedState
  * --> stoppedState: stopEvent
  runningState --> runningState: tickEvent
  stoppedState --> runningState: startEvent
```

**Features**:
- Nested state machines: 2-layer structure of Scene and Character
- Auto-transition: Automatic transition from Result to Title after time elapsed
- Game loop: Call `eventloop.Dispatcher.FastForward()` every frame

## Hints

### Recommended Patterns

1. **Use Gateway Struct Pattern**: Encapsulate with `*Data` embedding + private `machine`
2. **Leverage Type Aliases**: Clarify type definitions
3. **Split by State into Files/Packages**: Improve readability in large projects
4. **Clarify Responsibilities**: Separate data roles and define state machine scope
5. **Utilize Nested Structures**: Build complex systems through hierarchies
6. **Validate with Guard Conditions**: Prevent invalid state transitions
7. **Use Timers via Dispatcher**: Keep timer callbacks serialized with other calls

### Common Mistakes

1. **Direct Trigger in Entry**: return `state.Trigger(event)` from `Entry` instead
2. **Timers without Dispatcher**: Use `machine.AfterFunc` instead of `time.AfterFunc`
3. **Forgetting OnExit Registration**: Don't forget to register guard conditions in `Entry`
4. **Missing State Transition Graph Definitions**: Explicitly define all transitions
5. **State Explosion**: Consider hierarchies when there are too many states
6. **InvokeFunc or Task.Wait from a callback**: Use `machine.AfterFunc(dispatcher, 0, ...)` instead

### When to Use State Machines

- When you need **multiple states with complex transition rules** and **time-based auto-transitions** (timeouts)

### Avoiding State Explosion

Solutions when state count grows too large:

1. **Hierarchies**: Use nested state machines (Scene and Character layer examples)
2. **State Consolidation**: Consolidate similar-behaving states and distinguish by data

### References

- [vending-machine implementation example](../examples/vending-machine/main.go)
