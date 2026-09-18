package state_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raiich/kazura/state"
	"github.com/raiich/kazura/task/eventloop"
)

// Not guaranteed: real-time timer accuracy (the synchronous model driven by
// eventloop.Dispatcher's FastForward only), firing order across several Dispatchers, a
// Dispatcher that fires timers reentrantly, the relative order of several timers
// registered at the same time (the Dispatcher's contract).

func TestEntryMachine_OnExit(t *testing.T) {
	type NextEvent struct{}
	type BackEvent struct{}
	type SelfEvent struct{}
	type SpecialEvent struct{ data string }

	t.Run("the exit action runs between the two Entry calls", func(t *testing.T) {
		var order []string
		to := &TestState{
			name: "to",
			entry: func(*EntryMachine, Event) state.Command {
				order = append(order, "to-entry")
				return nil
			},
		}
		from := &TestState{
			name: "from",
			entry: func(m *EntryMachine, _ Event) state.Command {
				order = append(order, "from-entry")
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					order = append(order, "exit")
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](from, On[NextEvent](from, to))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		assert.Equal(t, []string{"from-entry", "exit", "to-entry"}, order)
	})

	t.Run("the exit action receives the triggering event", func(t *testing.T) {
		var exitEvents []Event
		to := &TestState{name: "to"}
		from := &TestState{
			name: "from",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(event Event) *state.Guarded {
					exitEvents = append(exitEvents, event)
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](from, On[SpecialEvent](from, to))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(SpecialEvent{data: "test"}))
		assert.Equal(t, []Event{SpecialEvent{data: "test"}}, exitEvents)
	})

	t.Run("a Guarded keeps the state and guards the following event with the same callback", func(t *testing.T) {
		guarded := &state.Guarded{Reason: errors.New("blocked")}
		exitCount := 0
		entryCount := 0
		to := &TestState{
			name: "to",
			entry: func(*EntryMachine, Event) state.Command {
				entryCount++
				return nil
			},
		}
		from := &TestState{
			name: "from",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					exitCount++
					return guarded
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](from, On[NextEvent](from, to))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		for range 2 {
			err := machine.Trigger(NextEvent{})
			var blocked *state.Guarded
			require.ErrorAs(t, err, &blocked)
			assert.Same(t, guarded, blocked)

			current, err := machine.CurrentState()
			require.NoError(t, err)
			assert.Equal(t, from, current)
		}
		assert.Equal(t, 2, exitCount)
		assert.Equal(t, 0, entryCount)
	})

	t.Run("Guarded with a nil Reason reports the default message", func(t *testing.T) {
		to := &TestState{name: "to"}
		from := &TestState{
			name: "from",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					return &state.Guarded{}
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](from, On[NextEvent](from, to))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		err = machine.Trigger(NextEvent{})
		assert.EqualError(t, err, "state transition blocked")
	})

	t.Run("the exit action runs exactly once when leaving", func(t *testing.T) {
		// one counter per visit, in visit order
		var exitCounts []int
		count := func(m *EntryMachine) {
			visit := len(exitCounts)
			exitCounts = append(exitCounts, 0)
			require.NoError(t, m.OnExit(func(Event) *state.Guarded {
				exitCounts[visit]++
				return nil
			}))
		}
		entry := func(m *EntryMachine, _ Event) state.Command {
			count(m)
			return nil
		}
		a := &TestState{name: "a", entry: entry}
		b := &TestState{name: "b", entry: entry}
		g, err := state.NewGraph[State](a, On[NextEvent](a, b), On[BackEvent](b, a))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		require.NoError(t, machine.Trigger(BackEvent{}))
		assert.Equal(t, []int{1, 1, 0}, exitCounts)
	})

	t.Run("a second OnExit in the same visit returns ErrExitActionRegistered", func(t *testing.T) {
		exitCount := 0
		to := &TestState{name: "to"}
		from := &TestState{
			name: "from",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					exitCount++
					return nil
				}))
				err := m.OnExit(func(Event) *state.Guarded {
					t.Error("the second exit action must not be registered")
					return nil
				})
				assert.ErrorIs(t, err, state.ErrExitActionRegistered)
				return nil
			},
		}
		g, err := state.NewGraph[State](from, On[NextEvent](from, to))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		assert.Equal(t, 1, exitCount)
	})

	t.Run("CurrentState inside the exit action is the state being left", func(t *testing.T) {
		var machine *state.Machine[State, *TestValue]
		var leaving State
		to := &TestState{name: "to"}
		from := &TestState{
			name: "from",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					current, err := machine.CurrentState()
					require.NoError(t, err)
					leaving = current
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](from, On[NextEvent](from, to))
		require.NoError(t, err)

		machine = state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		assert.Equal(t, from, leaving)
	})

	t.Run("OnExit from inside the exit action returns ErrExitActionRegistered", func(t *testing.T) {
		to := &TestState{name: "to"}
		from := &TestState{
			name: "from",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					err := m.OnExit(func(Event) *state.Guarded { return nil })
					assert.ErrorIs(t, err, state.ErrExitActionRegistered)
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](from, On[NextEvent](from, to))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, to, current)
	})

	t.Run("OnExit on a left EntryMachine returns ErrStateLeft and the destination gets no guard", func(t *testing.T) {
		var left *EntryMachine
		to := &TestState{name: "to"}
		from := &TestState{
			name: "from",
			entry: func(m *EntryMachine, _ Event) state.Command {
				left = m
				return nil
			},
		}
		g, err := state.NewGraph[State](from, On[NextEvent](from, to), On[BackEvent](to, from))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(NextEvent{}))

		err = left.OnExit(func(Event) *state.Guarded {
			return &state.Guarded{Reason: errors.New("blocked")}
		})
		assert.ErrorIs(t, err, state.ErrStateLeft)

		require.NoError(t, machine.Trigger(BackEvent{}))
	})

	t.Run("OnExit on a handle left by a self transition returns ErrStateLeft", func(t *testing.T) {
		var first *EntryMachine
		var exits []string
		self := &TestState{name: "self"}
		self.entry = func(m *EntryMachine, _ Event) state.Command {
			visit := "second"
			if first == nil {
				first = m
				visit = "first"
			}
			require.NoError(t, m.OnExit(func(Event) *state.Guarded {
				exits = append(exits, visit)
				return nil
			}))
			return nil
		}
		next := &TestState{name: "next"}
		g, err := state.NewGraph[State](self, On[SelfEvent](self, self), On[NextEvent](self, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(SelfEvent{}))

		assert.ErrorIs(t, first.OnExit(func(Event) *state.Guarded { return nil }), state.ErrStateLeft)

		require.NoError(t, machine.Trigger(NextEvent{}))
		assert.Equal(t, []string{"first", "second"}, exits)
	})

	t.Run("OnExit on a handle left by Machine.Stop returns ErrStateLeft", func(t *testing.T) {
		var left *EntryMachine
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				left = m
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Stop())

		assert.ErrorIs(t, left.OnExit(func(Event) *state.Guarded { return nil }), state.ErrStateLeft)

		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Stop())
	})
}

func TestEntryMachine_AfterFunc(t *testing.T) {
	type NextEvent struct{}
	type SelfEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("the timer fires while the visit is current", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		fireCount := 0
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 5*time.Second, func(*AfterFuncMachine) {
					fireCount++
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(4*time.Second)))
		assert.Equal(t, 0, fireCount)

		require.NoError(t, dispatcher.FastForward(baseTime.Add(5*time.Second)))
		assert.Equal(t, 1, fireCount)
	})

	t.Run("the timers of the visit are canceled on a transition", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var fired []string
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 10*time.Second, func(*AfterFuncMachine) {
					fired = append(fired, "10s")
				}))
				require.NoError(t, m.AfterFunc(dispatcher, 12*time.Second, func(*AfterFuncMachine) {
					fired = append(fired, "12s")
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(NextEvent{}))

		require.NoError(t, dispatcher.FastForward(baseTime.Add(15*time.Second)))
		assert.Empty(t, fired)
	})

	t.Run("the timers of the visit are canceled on a self transition, and the new visit's timer fires", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		visits := 0
		var fired []int
		self := &TestState{name: "self"}
		self.entry = func(m *EntryMachine, _ Event) state.Command {
			visits++
			visit := visits
			require.NoError(t, m.AfterFunc(dispatcher, 10*time.Second, func(*AfterFuncMachine) {
				fired = append(fired, visit)
			}))
			return nil
		}
		g, err := state.NewGraph[State](self, On[SelfEvent](self, self))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))
		require.NoError(t, machine.Trigger(SelfEvent{}))

		require.NoError(t, dispatcher.FastForward(baseTime.Add(11*time.Second)))
		assert.Equal(t, []int{2}, fired)
	})

	t.Run("the timers of the visit survive a blocked transition and still fire", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		fireCount := 0
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					return &state.Guarded{Reason: errors.New("blocked")}
				}))
				require.NoError(t, m.AfterFunc(dispatcher, 2*time.Second, func(*AfterFuncMachine) {
					fireCount++
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.Error(t, machine.Trigger(NextEvent{}))

		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
		assert.Equal(t, 1, fireCount)
	})

	t.Run("AfterFunc from inside the exit action is canceled by the transition", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		fireCount := 0
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					assert.NoError(t, m.AfterFunc(dispatcher, 3*time.Second, func(*AfterFuncMachine) {
						fireCount++
					}))
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(NextEvent{}))

		require.NoError(t, dispatcher.FastForward(baseTime.Add(5*time.Second)))
		assert.Equal(t, 0, fireCount)
	})

	t.Run("AfterFunc from inside the exit action survives a blocked transition", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		fireCount := 0
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					assert.NoError(t, m.AfterFunc(dispatcher, 3*time.Second, func(*AfterFuncMachine) {
						fireCount++
					}))
					return &state.Guarded{Reason: errors.New("blocked")}
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.Error(t, machine.Trigger(NextEvent{}))

		require.NoError(t, dispatcher.FastForward(baseTime.Add(5*time.Second)))
		assert.Equal(t, 1, fireCount)
	})

	t.Run("multiple timers fire in delay order", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var fired []string
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				for _, delay := range []int{3, 1, 2} {
					name := fmt.Sprintf("%ds", delay)
					require.NoError(t, m.AfterFunc(dispatcher, time.Duration(delay)*time.Second, func(*AfterFuncMachine) {
						fired = append(fired, name)
					}))
				}
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(3*time.Second)))
		assert.Equal(t, []string{"1s", "2s", "3s"}, fired)
	})

	t.Run("a zero delay timer fires on the next dispatch", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		fireCount := 0
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 0, func(*AfterFuncMachine) {
					fireCount++
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		assert.Equal(t, 0, fireCount)

		require.NoError(t, dispatcher.FastForward(baseTime))
		assert.Equal(t, 1, fireCount)
	})

	t.Run("a fired timer is no longer held by the visit", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(*AfterFuncMachine) {}))
				require.NoError(t, m.AfterFunc(dispatcher, 2*time.Second, func(*AfterFuncMachine) {}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		assert.Equal(t, 2, machine.ActiveTimerCount())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))
		assert.Equal(t, 1, machine.ActiveTimerCount())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
		assert.Equal(t, 0, machine.ActiveTimerCount())
	})

	t.Run("many timers all fire and leave no active timer", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		const timers = 1000
		fireCount := 0
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				for i := range timers {
					require.NoError(t, m.AfterFunc(dispatcher, time.Duration(i+1)*time.Millisecond, func(*AfterFuncMachine) {
						fireCount++
					}))
				}
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		assert.Equal(t, timers, machine.ActiveTimerCount())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(timers*time.Millisecond)))
		assert.Equal(t, timers, fireCount)
		assert.Equal(t, 0, machine.ActiveTimerCount())
	})

	t.Run("AfterFunc on a left EntryMachine returns ErrStateLeft and the destination gets no timer", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var left *EntryMachine
		fireCount := 0
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				left = m
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(NextEvent{}))

		err = left.AfterFunc(dispatcher, 1*time.Second, func(*AfterFuncMachine) {
			fireCount++
		})
		assert.ErrorIs(t, err, state.ErrStateLeft)
		assert.Equal(t, 0, machine.ActiveTimerCount())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
		assert.Equal(t, 0, fireCount)
	})

	t.Run("AfterFunc on a handle left by a self transition returns ErrStateLeft", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var first *EntryMachine
		fireCount := 0
		self := &TestState{name: "self"}
		self.entry = func(m *EntryMachine, _ Event) state.Command {
			if first == nil {
				first = m
			}
			return nil
		}
		g, err := state.NewGraph[State](self, On[SelfEvent](self, self))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(SelfEvent{}))

		err = first.AfterFunc(dispatcher, 1*time.Second, func(*AfterFuncMachine) {
			fireCount++
		})
		assert.ErrorIs(t, err, state.ErrStateLeft)

		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
		assert.Equal(t, 0, fireCount)
	})

	t.Run("AfterFunc on a handle left by Machine.Stop returns ErrStateLeft", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var left *EntryMachine
		fireCount := 0
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				left = m
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Stop())

		err = left.AfterFunc(dispatcher, 1*time.Second, func(*AfterFuncMachine) {
			fireCount++
		})
		assert.ErrorIs(t, err, state.ErrStateLeft)

		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
		assert.Equal(t, 0, fireCount)
	})
}
