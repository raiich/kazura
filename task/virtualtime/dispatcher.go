// Package virtualtime provides a Dispatcher that runs its functions on a
// simulated clock, which advances only through [Dispatcher.FastForward].
// It enables precise timing control for applications like game loops and testing.
package virtualtime

import (
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/raiich/kazura/task"
)

// ErrRunning reports a [Dispatcher.FastForward] while one runs.
var ErrRunning = errors.New("virtualtime: FastForward is already running")

// The Dispatcher has no Now. The caller of FastForward owns the clock, so a
// wrapper that needs the time, such as pausable, takes the caller's clock
// rather than the simulated one.

// Dispatcher runs the functions submitted to it only while
// [Dispatcher.FastForward] advances its simulated time.
type Dispatcher struct {
	running atomic.Bool

	mu sync.Mutex
	// advancedTo is the time FastForward has advanced to.
	advancedTo time.Time

	ended bool
	// Ordered list of scheduled tasks (earliest first)
	tasks []scheduledTask
}

// FastForward advances the time to the specified time and executes all tasks
// that are scheduled to run during this time period. Useful for game loops
// and controlled time progression scenarios.
//
// A FastForward while one runs returns [ErrRunning]. When a task panics, the
// dispatcher stops as [task.Dispatcher.AfterFunc] describes and FastForward
// returns an error describing the panic.
func (d *Dispatcher) FastForward(to time.Time) error {
	if !d.running.CompareAndSwap(false, true) {
		return ErrRunning
	}
	defer d.running.Store(false)
	for {
		head, ok := d.proceedAndDequeue(to)
		if !ok {
			return nil
		}
		if err := head.run(); err != nil {
			d.shutdown()
			return err
		}
	}
}

// proceedAndDequeue advances time and dequeues the next task if available.
// Returns the next task and whether one was due by end.
func (d *Dispatcher) proceedAndDequeue(end time.Time) (*pendingTask, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// A shut-down dispatcher runs nothing further. Queued tasks stay so a
	// Timer.Stop can still cancel them, but they never execute.
	if d.ended {
		return nil, false
	}

	head, ok := d.dequeue(end)
	if !ok {
		if end.After(d.advancedTo) {
			// No more tasks before end time, advance to end
			d.advancedTo = end
		}
		return nil, false
	}
	return head, true
}

// dequeue removes and returns the earliest scheduled task if it should execute before end time.
// Returns the task and whether one was available within the time limit.
func (d *Dispatcher) dequeue(end time.Time) (*pendingTask, bool) {
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
	d.advancedTo = head.at
	return head.task, true
}

func (d *Dispatcher) shutdown() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Queued tasks stay (not removed) so a Timer.Stop still reports it prevented
	// execution; the ended gate keeps them from running.
	d.ended = true
}

// AfterFunc schedules f to run once the simulated time has advanced by duration.
//
// See [task.Timer.Stop] for Stop semantics.
func (d *Dispatcher) AfterFunc(duration time.Duration, f func()) task.Timer {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Not before advancedTo, which dequeue would rewind the clock to.
	at := d.advancedTo.Add(max(duration, 0))
	t := &pendingTask{fn: f}
	d.enqueue(at, t)
	return &taskTimer{
		dispatcher: d,
		task:       t,
	}
}

func (d *Dispatcher) enqueue(at time.Time, pending *pendingTask) {
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
func (d *Dispatcher) dropTask(task *pendingTask) bool {
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
func NewDispatcher(start time.Time) *Dispatcher {
	return &Dispatcher{
		advancedTo: start,
	}
}

// scheduledTask represents a task scheduled to execute at a specific time.
type scheduledTask struct {
	at   time.Time
	task *pendingTask
}

// taskTimer implements the task.Timer interface for canceling scheduled tasks.
type taskTimer struct {
	dispatcher *Dispatcher
	task       *pendingTask
}

// Stop cancels the scheduled task; see [task.Timer.Stop]. A task left queued
// by a shutdown never runs, so Stop still reports that it prevented the function.
func (t *taskTimer) Stop() bool {
	return t.dispatcher.dropTask(t.task)
}

// pendingTask is a function queued for serialized execution. Its pointer
// identifies the entry that a taskTimer drops.
type pendingTask struct {
	fn func()
}

// run executes the function. A panic is recovered and returned as an error.
func (t *pendingTask) run() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	t.fn()
	return nil
}
