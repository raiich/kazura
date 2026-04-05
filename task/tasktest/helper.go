package tasktest

import (
	"testing"
	"testing/synctest"
)

// WithSyncTest wraps a test function with synctest.Test, recovering from deadlock
// panics and converting them to test failures, allowing subsequent tests to continue.
//
// This prevents a single deadlock bug from stopping the entire test suite.
// The test will still fail, but with a clear error message, and other tests
// will run normally.
//
// Usage:
//
//	t.Run("test description", WithSyncTest(func(t *testing.T) {
//	    // Your synctest code here
//	}))
func WithSyncTest(f func(t *testing.T)) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("synctest panic recovered: %v\n\nThis typically indicates a deadlock in the test. All goroutines in the synctest bubble are permanently blocked.", r)
			}
		}()
		synctest.Test(t, f)
	}
}
