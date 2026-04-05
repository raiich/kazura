package state

import (
	"time"

	"github.com/raiich/kazura/task"
)

type timerGroup struct {
	// Active timers
	timers []*timerEntry
}

// AfterFunc schedules a function to execute after the specified duration.
// The function will not execute if Clear is called before the timer fires.
func (m *timerGroup) AfterFunc(dispatcher task.Dispatcher, d time.Duration, f func()) {
	entry := &timerEntry{}
	entry.timer = dispatcher.AfterFunc(d, func() {
		f()
		m.removeTimer(entry)
	})
	m.timers = append(m.timers, entry)
}

// removeTimer removes the specified timer from the active timers list.
func (m *timerGroup) removeTimer(fired *timerEntry) {
	for i, timer := range m.timers {
		if timer == fired {
			m.timers[i] = m.timers[len(m.timers)-1]
			m.timers[len(m.timers)-1] = nil // avoid retaining reference
			m.timers = m.timers[:len(m.timers)-1]
			return
		}
	}
}

func (m *timerGroup) Clear() {
	for _, timer := range m.timers {
		timer.timer.Stop()
	}
	m.timers = nil
}

// timerEntry wraps task.Timer for safe pointer comparison in removeTimer.
// Interface comparison can panic if the underlying type is not comparable,
// so we compare *timerEntry pointers instead.
type timerEntry struct {
	// The underlying timer implementation
	timer task.Timer
}
