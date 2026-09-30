package state_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/raiich/kazura/state"
	"github.com/raiich/kazura/task/virtualtime"
)

// The timer benchmarks share their shape with the tasktest ones, scheduling one
// timer and advancing the time to fire it, so the difference from the
// virtualtime result is the cost the Manager or the Machine adds.

// delay is the delay of the timer under measurement.
const delay = time.Millisecond

// noopTracer is a Tracer that does nothing, so a benchmark measures the
// notification alone.
type noopTracer struct{}

func (noopTracer) Trace(Transition) {}

// BenchmarkMachine_Trigger measures one transition between two states.
func BenchmarkMachine_Trigger(b *testing.B) {
	type Ping struct{}
	baseTime := time.Unix(0, 0)

	// run builds the graph in which Ping moves the machine between initial and
	// next, launches it, and measures one Trigger of Ping.
	run := func(b *testing.B, initial, next *TestState, opts ...state.Option[State]) {
		g, err := state.NewGraph[State](initial, On[Ping](initial, next), On[Ping](next, initial))
		if err != nil {
			b.Fatal(err)
		}
		machine := state.NewMachine(g, &TestValue{}, opts...)
		if err := machine.Launch(); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if err := machine.Trigger(Ping{}); err != nil {
				b.Fatal(err)
			}
		}
	}

	b.Run("plain", func(b *testing.B) {
		run(b, &TestState{name: "initial"}, &TestState{name: "next"})
	})

	b.Run("exit action", func(b *testing.B) {
		entry := func(m *EntryMachine, _ Event) state.Command {
			if err := m.OnExit(func(Event) *state.Guarded { return nil }); err != nil {
				b.Fatal(err)
			}
			return nil
		}
		run(b, &TestState{name: "initial", entry: entry}, &TestState{name: "next", entry: entry})
	})

	b.Run("timer", func(b *testing.B) {
		dispatcher := virtualtime.NewDispatcher(baseTime)
		// The timer never fires: leaving the state cancels it.
		entry := func(m *EntryMachine, _ Event) state.Command {
			if err := m.AfterFunc(dispatcher, time.Hour, func(*AfterFuncMachine) {}); err != nil {
				b.Fatal(err)
			}
			return nil
		}
		run(b, &TestState{name: "initial", entry: entry}, &TestState{name: "next", entry: entry})
	})

	b.Run("tracer", func(b *testing.B) {
		run(b, &TestState{name: "initial"}, &TestState{name: "next"}, state.WithTracer[State](noopTracer{}))
	})

	b.Run("chain", func(b *testing.B) {
		// The Entry of next moves the machine back, so one Trigger is two transitions.
		next := &TestState{
			name: "next",
			entry: func(*EntryMachine, Event) state.Command {
				return state.Trigger(Ping{})
			},
		}
		run(b, &TestState{name: "initial"}, next)
	})
}

// BenchmarkMachine_AfterFunc schedules one timer from the handle of the current
// visit and advances the time to fire it.
func BenchmarkMachine_AfterFunc(b *testing.B) {
	baseTime := time.Unix(0, 0)
	dispatcher := virtualtime.NewDispatcher(baseTime)
	// The handle stays valid while the machine stays in the state.
	var handle *EntryMachine
	initial := &TestState{
		name: "initial",
		entry: func(m *EntryMachine, _ Event) state.Command {
			handle = m
			return nil
		},
	}
	g, err := state.NewGraph[State](initial)
	if err != nil {
		b.Fatal(err)
	}
	machine := state.NewMachine(g, &TestValue{})
	if err := machine.Launch(); err != nil {
		b.Fatal(err)
	}

	now := baseTime
	b.ReportAllocs()
	for b.Loop() {
		now = now.Add(delay)
		if err := handle.AfterFunc(dispatcher, delay, func(*AfterFuncMachine) {}); err != nil {
			b.Fatal(err)
		}
		if err := dispatcher.FastForward(now); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAfterFuncMachine_Trigger measures one timeout: each state schedules a
// timer whose callback triggers the transition to the other state, and the
// benchmark advances the time to fire it.
func BenchmarkAfterFuncMachine_Trigger(b *testing.B) {
	type Ping struct{}
	baseTime := time.Unix(0, 0)
	dispatcher := virtualtime.NewDispatcher(baseTime)
	// Shared by every timer, so the benchmark allocates no closure of its own
	// per Entry.
	trigger := func(m *AfterFuncMachine) {
		if err := m.Trigger(Ping{}); err != nil {
			b.Fatal(err)
		}
	}
	entry := func(m *EntryMachine, _ Event) state.Command {
		if err := m.AfterFunc(dispatcher, delay, trigger); err != nil {
			b.Fatal(err)
		}
		return nil
	}
	initial := &TestState{name: "initial", entry: entry}
	next := &TestState{name: "next", entry: entry}
	g, err := state.NewGraph[State](initial, On[Ping](initial, next), On[Ping](next, initial))
	if err != nil {
		b.Fatal(err)
	}
	machine := state.NewMachine(g, &TestValue{})
	if err := machine.Launch(); err != nil {
		b.Fatal(err)
	}

	now := baseTime
	b.ReportAllocs()
	for b.Loop() {
		now = now.Add(delay)
		if err := dispatcher.FastForward(now); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkManager_AfterFunc schedules one timer for the current value and
// advances the time to fire it.
func BenchmarkManager_AfterFunc(b *testing.B) {
	baseTime := time.Unix(0, 0)
	dispatcher := virtualtime.NewDispatcher(baseTime)
	manager := state.NewManager[int]()

	now := baseTime
	b.ReportAllocs()
	for b.Loop() {
		now = now.Add(delay)
		manager.AfterFunc(dispatcher, delay, func() {})
		if err := dispatcher.FastForward(now); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkManager_Set replaces the value while it holds a number of timers,
// which Set cancels. Scheduling them is part of the measurement.
func BenchmarkManager_Set(b *testing.B) {
	for _, timers := range []int{0, 1, 10} {
		b.Run(fmt.Sprintf("timers=%d", timers), func(b *testing.B) {
			dispatcher := virtualtime.NewDispatcher(time.Unix(0, 0))
			manager := state.NewManager[int]()
			b.ReportAllocs()
			for b.Loop() {
				for range timers {
					manager.AfterFunc(dispatcher, time.Hour, func() {})
				}
				manager.Set(1)
			}
		})
	}
}
