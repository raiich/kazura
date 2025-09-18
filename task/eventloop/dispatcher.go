// Package eventloop provides a controllable time-based event dispatcher.
// It enables precise timing control for applications like game loops and testing.
package eventloop

import (
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/raiich/kazura/task"
)

// Dispatcher manages scheduled tasks with controllable time progression.
// It maintains an ordered queue of tasks and allows manual time advancement
// for applications requiring precise timing control, such as game loops.
type Dispatcher struct {
	// mu is used for concurrent access
	mu sync.Mutex
	// Current simulated time
	now time.Time
	// Ordered list of scheduled tasks (earliest first)
	tasks []*scheduledTask
}

// FastForward advances the time to the specified time and executes all tasks
// that are scheduled to run during this time period. Useful for game loops
// and controlled time progression scenarios.
//
// NOTE: This method is intended to be called from a single goroutine only.
// Concurrent calls from multiple goroutines may lead to race conditions.
func (d *Dispatcher) FastForward(to time.Time) error {
	for {
		head, ok, err := d.proceedAndDequeue(to)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if head == nil {
			return nil
		}
		if err := d.safeExec(head); err != nil {
			return err
		}
	}
}

// safeExec executes a scheduled task with panic recovery.
// If the task panics, it recovers and returns an error instead.
func (d *Dispatcher) safeExec(task *scheduledTask) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	task.Exec()
	return nil
}

// proceedAndDequeue advances time and dequeues the next task if available.
// Returns the next task and whether one was found within the time limit and an error if any occurs during processing.
func (d *Dispatcher) proceedAndDequeue(end time.Time) (*scheduledTask, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if end.Before(d.now) {
		return nil, false, fmt.Errorf("unprocessable time: now=%v, to=%v", d.now, end)
	}

	head, ok := d.dequeue(end)
	if !ok {
		// No more tasks before end time, advance to end
		d.now = end
		return nil, false, nil
	}
	// Advance time to the task's scheduled time
	d.now = head.at
	return head, true, nil
}

// dequeue removes and returns the earliest scheduled task if it should execute before end time.
// Returns the task and whether one was available within the time limit.
func (d *Dispatcher) dequeue(end time.Time) (*scheduledTask, bool) {
	if len(d.tasks) == 0 {
		return nil, false
	}
	head, tail := d.tasks[0], d.tasks[1:]
	// Check if the earliest task should execute after the end time
	if end.Before(head.at) {
		return nil, false
	}
	// Remove the task from the queue
	d.tasks = tail
	return head, true
}

// AfterFunc schedules a function to be executed after the specified duration.
// Returns a Timer that can be used to cancel the scheduled task.
// The task is inserted into the queue maintaining chronological order.
func (d *Dispatcher) AfterFunc(duration time.Duration, f func()) task.Timer {
	d.mu.Lock()
	defer d.mu.Unlock()

	at := d.now.Add(duration)
	entry := &scheduledTask{
		at:   at,
		task: f,
	}
	// Find the correct insertion point to maintain chronological order
	i := 0
	for i < len(d.tasks) {
		if at.Before(d.tasks[i].at) {
			break
		}
		i++
	}
	// Insert the task at the correct position
	d.insertTask(i, entry)
	return &taskTimer{
		dispatcher: d,
		entry:      entry,
	}
}

func (d *Dispatcher) insertTask(i int, entry *scheduledTask) {
	if len(d.tasks) == i {
		d.tasks = append(d.tasks, entry)
		return
	}
	d.tasks = append(d.tasks[:i+1], d.tasks[i:]...)
	d.tasks[i] = entry
}

// dropTask removes a specific scheduled task from the queue.
// Returns true if the task was found and removed, false otherwise.
func (d *Dispatcher) dropTask(task *scheduledTask) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, e := range d.tasks {
		if e == task {
			// Remove the task by slicing around it
			d.tasks = append(d.tasks[:i], d.tasks[i+1:]...)
			return true
		}
	}
	return false
}

// NewDispatcher creates a new Dispatcher with the specified time as the starting point.
func NewDispatcher(now time.Time) *Dispatcher {
	return &Dispatcher{
		now: now,
	}
}

// scheduledTask represents a task scheduled to execute at a specific time.
type scheduledTask struct {
	// When the task should execute
	at time.Time
	// The function to execute
	task func()
}

// Exec executes the scheduled task.
func (t *scheduledTask) Exec() {
	t.task()
}

// taskTimer implements the task.Timer interface for canceling scheduled tasks.
type taskTimer struct {
	// Reference to the dispatcher that owns this timer
	dispatcher *Dispatcher
	// The scheduled task this timer controls
	entry *scheduledTask
}

// Stop cancels the scheduled task.
// Returns true if the task was successfully canceled, false if it was already executed or canceled.
func (t *taskTimer) Stop() bool {
	return t.dispatcher.dropTask(t.entry)
}
