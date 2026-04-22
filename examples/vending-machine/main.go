// Package main demonstrates a vending machine implementation using kazura state machine.
// This example showcases state transitions, guard conditions, timeouts, and event handling.
package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/raiich/kazura/must"
	"github.com/raiich/kazura/state"
	"github.com/raiich/kazura/state/graph"
	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/eventloop"
)

var log = slog.Default()

// stateGraph defines the vending machine state machine configuration.
var stateGraph = must.Must(state.NewGraph[State](
	InitialState{},
	On[CoinEvent](InitialState{}, WaitingState{}),
	On[CoinEvent](WaitingState{}, WaitingState{}),
	On[DoneEvent](WaitingState{}, InitialState{}),
	On[*ButtonEvent](WaitingState{}, PouringState{}),
	On[DoneEvent](PouringState{}, InitialState{}),
))

// InitialState represents the idle state of the vending machine.
// The machine is ready to accept coins.
type InitialState struct {
}

func (s InitialState) Entry(machine *EntryMachine, event Event) {
	machine.Value().Coins = 0 // reset coins
}

// transitionLogger implements state.Tracer and logs every state transition.
// Using WithTracer keeps each Entry method free of transition-logging boilerplate.
type transitionLogger struct{}

func (transitionLogger) Trace(from, to State, event state.Event) {
	log.Info("transition",
		"from", fmt.Sprintf("%T", from),
		"to", fmt.Sprintf("%T", to),
		"event", event)
}

// WaitingState represents the state where the machine waits for user interaction.
// It accepts additional coins and validates item selection based on available funds.
// Automatically times out after 10 seconds of inactivity.
type WaitingState struct {
}

func (s WaitingState) Entry(machine *EntryMachine, event Event) {
	vendingMachine := machine.Value()

	switch event.(type) {
	case CoinEvent:
		vendingMachine.Coins++
		log.Info("coin", "count", vendingMachine.Coins)
	}

	// Set up exit guard to validate item purchases
	must.NoError(machine.OnExit(func(machine *ExitMachine, event state.Event) *state.Guarded {
		switch e := event.(type) {
		case CoinEvent:
			return nil // nothing to do
		case *ButtonEvent:
			switch {
			case e.Item == "coffee" && vendingMachine.Coins < 2:
				return Guarded("2 coin(s) for %v, but %d", e.Item, vendingMachine.Coins)
			}
		}
		return nil
	}))

	// Set up timeout to return to initial state
	machine.AfterFunc(vendingMachine.Dispatcher, 10*time.Second, func(machine *AfterFuncMachine) {
		must.NoError(machine.Trigger(DoneEvent("timeout")))
	})
}

// PouringState represents the state where the machine is dispensing the selected item.
// Automatically transitions back to initial state when pouring is complete.
type PouringState struct{}

func (s PouringState) Entry(machine *EntryMachine, event state.Event) {
	log.Info("pouring", "item", event.(*ButtonEvent).Item)

	must.NoError(machine.AfterEntry(func(machine *AfterEntryMachine) {
		// done pouring
		must.NoError(machine.Trigger(DoneEvent("done")))
	}))
}

// CoinEvent represents a coin insertion event.
type CoinEvent int

// ButtonEvent represents an item selection button press.
type ButtonEvent struct {
	Item string
}

// DoneEvent represents completion or cancellation events.
type DoneEvent string

// VendingMachine holds the machine's state data.
type VendingMachine struct {
	Coins      int
	Dispatcher task.Dispatcher
}

func main() {
	log.Info("state diagram:\n```mermaid\n" + graph.Dump(stateGraph) + "\n```")
	baseTime := time.Now()
	dispatcher := eventloop.NewDispatcher(baseTime)
	vendingMachine := VendingMachine{
		Dispatcher: dispatcher,
	}
	machine := state.NewMachine(stateGraph, &vendingMachine, state.WithTracer[State](transitionLogger{}))
	must.NoError(machine.Launch())

	// Scenario 1: Buy water (1 coin required)
	log.Info("scenario: basic (pouring water)")
	{
		must.NoError(machine.Trigger(CoinEvent(1)))
		must.NoError(machine.Trigger(&ButtonEvent{Item: "water"}))
	}
	log.Info("---")

	// Scenario 2: Buy coffee (2 coins required)
	log.Info("scenario: basic (pouring coffee)")
	{
		must.NoError(machine.Trigger(CoinEvent(1)))
		must.NoError(machine.Trigger(CoinEvent(2)))
		must.NoError(machine.Trigger(&ButtonEvent{Item: "coffee"}))
	}
	log.Info("---")

	// Scenario 3: Insufficient coins - guard condition prevents transition
	log.Info("scenario: insufficient coins and cancel")
	{
		must.NoError(machine.Trigger(CoinEvent(1)))
		err := machine.Trigger(&ButtonEvent{Item: "coffee"})
		log.Info("insufficient coins", "error", err)
		must.NoError(machine.Trigger(DoneEvent("cancel")))
	}
	log.Info("---")

	// Scenario 4: Timeout after coin insertion
	log.Info("scenario: timeout")
	{
		must.NoError(machine.Trigger(CoinEvent(1)))
		must.NoError(dispatcher.FastForward(baseTime.Add(10 * time.Second)))
	}
}
