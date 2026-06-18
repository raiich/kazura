package queue

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/raiich/kazura/task"
)

// TestDispatcher_InvokeRacingShutdown stresses the send race: InvokeFunc submits
// concurrently with Serve stopping, so a task can land in the queue after Serve's
// drain and must then be canceled by the drain in pendingTask.Wait. Run under
// -race, it guards against a double Cancel (close of a closed channel) and against
// Wait blocking on a stranded task. Each submission settles as nil (ran before
// stop), ErrCanceled (stranded then drained), or the canceled context's cause.
func TestDispatcher_InvokeRacingShutdown(t *testing.T) {
	for round := 0; round < 200; round++ {
		ctx, cancel := context.WithCancel(context.Background())
		d := NewDispatcher()
		served := make(chan struct{})
		go func() {
			_ = d.Serve(ctx)
			close(served)
		}()

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := d.InvokeFunc(func() {}).Wait(ctx)
				if err != nil && !errors.Is(err, task.ErrCanceled) && !errors.Is(err, context.Canceled) {
					t.Errorf("unexpected Wait error: %v", err)
				}
			}()
		}
		cancel()
		wg.Wait()
		<-served
	}
}
