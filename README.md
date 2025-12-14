# kazura

[ **English** | [日本語](docs/README.ja.md) ]

Go library that simplifies difficult stateful application development with asynchronous processing, complex state transitions, and timeouts.

## Problem & Solution

Stateful applications with asynchronous processing, complex state transitions, and timeouts are notoriously difficult to develop correctly.

kazura provides:
- **Serializing async tasks through dispatchers** (eliminating race conditions)
- **Managing state transitions and timeouts in unified machines** (preventing timing issues)
- **Requiring predefined state graphs** (making runtime behavior predictable and debuggable)

This approach makes complex stateful logic simple to implement, test, and extend.

## Features

- **Task serialization** via dispatchers to eliminate race conditions in async processing
- **Unified state machines** that handle both transitions and timeouts consistently
- **Predefined state graphs** for predictable runtime behavior and easy debugging
- **Virtual time support** for deterministic testing of time-dependent logic
- **Panic-based utilities** for clear distinction between bugs and recoverable errors

## Installation

```bash
go get github.com/raiich/kazura
```

## Quick Start

Let's build a vending machine state machine using kazura. This example demonstrates major features of kazura.

### 1. State Graph Definition

Define states and their transitions first. This makes runtime behavior predictable and debuggable.

```go
import (
    "github.com/raiich/kazura/state"
    "github.com/raiich/kazura/task/eventloop"
)

// Type aliases for better readability
type State = state.State[*VendingMachine]
type Event = state.Event

// Define the state graph
stateGraph := state.NewGraph[State](
    InitialState{},  // Initial state
    On[CoinEvent](InitialState{}, WaitingState{}),      // Coin insertion -> waiting
    On[CoinEvent](WaitingState{}, WaitingState{}),      // Additional coins
    On[DoneEvent](WaitingState{}, InitialState{}),      // Cancel/timeout
    On[*ButtonEvent](WaitingState{}, PouringState{}),   // Button press -> pouring
    On[DoneEvent](PouringState{}, InitialState{}),      // Pouring complete -> initial
)
```

State diagram:
```mermaid
stateDiagram-v2
  [*] --> InitialState
  InitialState --> WaitingState: CoinEvent
  PouringState --> InitialState: DoneEvent
  WaitingState --> InitialState: DoneEvent
  WaitingState --> PouringState: ButtonEvent
  WaitingState --> WaitingState: CoinEvent
```

### 2. State Implementation

Each state defines transition behavior in its `Entry` method.

```go
// Initial state: machine is idle
type InitialState struct{}

func (s InitialState) Entry(machine *EntryMachine, event Event) {
    machine.Value().Coins = 0  // Reset coin count
}

// Waiting state: accepts coins and item selection
type WaitingState struct{}

func (s WaitingState) Entry(machine *EntryMachine, event Event) {
    vendingMachine := machine.Value()

    // Handle coin events
    switch event.(type) {
    case CoinEvent:
        vendingMachine.Coins++
        slog.Info("coin", "count", vendingMachine.Coins)
    }

    // Guard conditions: conditionally control state transitions
    machine.OnExit(func(machine *ExitMachine, event Event) *state.Guarded {
        switch e := event.(type) {
        case *ButtonEvent:
            // Coffee requires 2 coins
            if e.Item == "coffee" && vendingMachine.Coins < 2 {
                return &state.Guarded{
                    Reason: fmt.Errorf("2 coin(s) for %v, but %d", e.Item, vendingMachine.Coins),
                }
            }
        }
        return nil  // Allow transition
    })

    // Timeout handling: automatically return to initial state after 10 seconds
    machine.AfterFunc(vendingMachine.Dispatcher, 10*time.Second, func(machine *AfterFuncMachine) {
        machine.Trigger(DoneEvent("timeout"))
    })
}

// Pouring state: dispense the selected item
type PouringState struct{}

func (s PouringState) Entry(machine *EntryMachine, event Event) {
    slog.Info("pouring", "item", event.(*ButtonEvent).Item)

    // Asynchronous processing: executed after state transition
    machine.AfterEntry(func(machine *AfterEntryMachine) {
        // Pouring complete
        machine.Trigger(DoneEvent("done"))
    })
}
```

### 3. Events and State Data Definition

```go
// Event type definitions
type CoinEvent int        // Coin insertion event
type ButtonEvent struct { // Button press event
    Item string
}
type DoneEvent string     // Completion/cancellation event

// State data
type VendingMachine struct {
    Coins      int
    Dispatcher Dispatcher
}
```

### 4. State Machine Execution

```go
func main() {
    // Create event loop dispatcher
    dispatcher := eventloop.NewDispatcher(time.Now())

    // Create and launch state machine
    vendingMachine := &VendingMachine{
        Dispatcher: dispatcher,
    }
    machine := state.NewMachine(stateGraph, vendingMachine)
    machine.Launch()

    // Scenario 1: Buy water (1 coin required)
    machine.Trigger(CoinEvent(1))
    machine.Trigger(&ButtonEvent{Item: "water"})

    // Scenario 2: Buy coffee (2 coins required)
    machine.Trigger(CoinEvent(1))
    machine.Trigger(CoinEvent(2))  // Additional coin
    machine.Trigger(&ButtonEvent{Item: "coffee"})

    // Scenario 3: Insufficient coins for coffee (rejected by guard condition)
    machine.Trigger(CoinEvent(1))
    err := machine.Trigger(&ButtonEvent{Item: "coffee"})  // Returns error

    // Scenario 4: Timeout test (using virtual time)
    machine.Trigger(CoinEvent(1))
    dispatcher.FastForward(time.Now().Add(10 * time.Second))  // Simulate 10 seconds
}
```

### 5. Key kazura Features

This example demonstrates the following kazura features:

- **State Graph Definition**: Predefine state transitions with `state.NewGraph`
- **State Transition Control**: Implement transition behavior in each state's `Entry` method
- **Guard Conditions**: Control conditional state transitions with `OnExit`
- **Timeout Handling**: Time-based automatic transitions with `AfterFunc`
- **Asynchronous Processing**: Post-transition async processing with `AfterEntry`
- **Event Dispatching**: Event ordering control with `eventloop.Dispatcher`
- **Virtual Time**: Time control for testing with `FastForward`

See the code example at [examples/vending-machine](examples/vending-machine/main.go).

## Packages

- **`state/`** - State machines that unify transitions and timeout handling, eliminating timing issues
- **`task/`** - Dispatchers that serialize async tasks (queue, mutex, eventloop) to prevent race conditions
- **`must/`** - Panic-based utilities that distinguish programming bugs from recoverable errors

## Documentation

TODO

- [Best Practices](docs/state-machine-best-practices.md)

## License

See [LICENSE](LICENSE) file.
