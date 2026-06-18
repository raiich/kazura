// Package eventloop provides a controllable time-based event dispatcher.
// It enables precise timing control for applications like game loops and testing.
package eventloop

import (
	"sync"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/internal"
)

// Dispatcher manages scheduled tasks with controllable time progression.
// It maintains an ordered queue of tasks and allows manual time advancement
// for applications requiring precise timing control, such as game loops.
type Dispatcher struct {
	// mu is used for concurrent access
	mu sync.Mutex
	// Current simulated time
	now time.Time

	ended bool
	// Ordered list of scheduled tasks (earliest first)
	tasks []scheduledTask
}

// FastForward advances the time to the specified time and executes all tasks
// that are scheduled to run during this time period. Useful for game loops
// and controlled time progression scenarios.
//
// NOTE: This method is intended to be called from a single goroutine only.
// Concurrent calls from multiple goroutines may lead to race conditions.
func (d *Dispatcher) FastForward(to time.Time) error {
	for {
		head, ok := d.proceedAndDequeue(to)
		if !ok {
			return nil
		}
		if err := head.Run(); err != nil {
			d.shutdown()
			return err
		}
	}
}

// proceedAndDequeue advances time and dequeues the next task if available.
// Returns the next task and whether one was found within the time limit and an error if any occurs during processing.
func (d *Dispatcher) proceedAndDequeue(end time.Time) (*internal.PendingTask, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// A shut-down dispatcher runs nothing further. Queued tasks (canceled by
	// shutdown, or submitted afterward) stay so a Timer.Stop can still cancel
	// them, but they never execute.
	if d.ended {
		return nil, false
	}

	head, ok := d.dequeue(end)
	if !ok {
		if end.After(d.now) {
			// No more tasks before end time, advance to end
			d.now = end
		}
		return nil, false
	}
	return head, true
}

// dequeue removes and returns the earliest scheduled task if it should execute before end time.
// Returns the task and whether one was available within the time limit.
func (d *Dispatcher) dequeue(end time.Time) (*internal.PendingTask, bool) {
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
	// Advance time to the task's scheduled time
	d.now = head.at
	return head.task, true
}

func (d *Dispatcher) shutdown() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ended = true
	// Cancel queued tasks so InvokeFunc waiters settle. They stay in the queue
	// (not removed) so an AfterFunc Timer.Stop still reports it prevented
	// execution; the ended gate keeps them from running.
	for _, entry := range d.tasks {
		entry.task.Cancel()
	}
}

// AfterFunc schedules a function to be executed after the specified duration.
// Returns a Timer that can be used to cancel the scheduled task.
// The task is inserted into the queue maintaining chronological order.
func (d *Dispatcher) AfterFunc(duration time.Duration, f func()) task.Timer {
	d.mu.Lock()
	defer d.mu.Unlock()

	at := d.now.Add(duration)
	t := internal.NewPendingTask(f)
	d.enqueue(at, t)
	return &taskTimer{
		dispatcher: d,
		task:       t,
	}
}

// InvokeFunc schedules f to run at the current simulated time, executed by the
// next FastForward.
func (d *Dispatcher) InvokeFunc(f func()) task.Task {
	d.mu.Lock()
	defer d.mu.Unlock()

	// A Task must settle, so a shut-down dispatcher cannot leave it queued-but-unrun
	// (Wait would block forever); settle it as canceled at submission instead.
	if d.ended {
		return internal.CanceledTask
	}

	t := internal.NewPendingTask(f)
	d.enqueue(d.now, t)
	return t
}

func (d *Dispatcher) enqueue(at time.Time, pending *internal.PendingTask) {
	// Find the correct insertion point to maintain chronological order
	i := 0
	for i < len(d.tasks) {
		if at.Before(d.tasks[i].at) {
			break
		}
		i++
	}
	// Insert the task at the correct position
	d.insertTask(i, scheduledTask{
		at:   at,
		task: pending,
	})
}

func (d *Dispatcher) insertTask(i int, entry scheduledTask) {
	if len(d.tasks) == i {
		d.tasks = append(d.tasks, entry)
		return
	}
	d.tasks = append(d.tasks[:i+1], d.tasks[i:]...)
	d.tasks[i] = entry
}

// dropTask removes a specific scheduled task from the queue.
// Returns true if the task was found and removed, false otherwise.
func (d *Dispatcher) dropTask(task *internal.PendingTask) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, e := range d.tasks {
		if e.task == task {
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

	task *internal.PendingTask
}

// taskTimer implements the task.Timer interface for canceling scheduled tasks.
type taskTimer struct {
	// Reference to the dispatcher that owns this timer
	dispatcher *Dispatcher
	// The scheduled task this timer controls
	task *internal.PendingTask
}

// Stop cancels the scheduled task.
// Returns true if the task was successfully canceled, false if it was already executed or canceled.
func (t *taskTimer) Stop() bool {
	return t.dispatcher.dropTask(t.task)
}
