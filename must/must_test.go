package must_test

import (
	"errors"
	"testing"

	"github.com/raiich/kazura/must"
	"github.com/stretchr/testify/assert"
)

func TestMust(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		var result any
		assert.NotPanicsf(t, func() {
			result = must.Must(42, error(nil))
		}, "Must should not panic with nil error")
		assert.Equal(t, 42, result)
	})

	t.Run("Error", func(t *testing.T) {
		assert.PanicsWithError(t, "test error", func() {
			must.Must(42, errors.New("test error"))
		})
	})
}

func TestNoError(t *testing.T) {
	t.Run("Nil", func(t *testing.T) {
		assert.NotPanicsf(t, func() {
			must.NoError(nil)
		}, "NoError should not panic with nil error")
	})

	t.Run("Error", func(t *testing.T) {
		assert.PanicsWithError(t, "test error", func() {
			must.NoError(errors.New("test error"))
		})
	})
}

func TestExist(t *testing.T) {
	t.Run("True", func(t *testing.T) {
		var result any
		assert.NotPanicsf(t, func() {
			result = must.Exist("value", true)
		}, "Exist should not panic with true")
		assert.Equal(t, "value", result)
	})

	t.Run("False", func(t *testing.T) {
		assert.PanicsWithValue(t, "value does not exist", func() {
			must.Exist("value", false)
		})
	})
}

func TestNotExist(t *testing.T) {
	t.Run("False", func(t *testing.T) {
		var result any
		assert.NotPanicsf(t, func() {
			result = must.NotExist("value", false)
		}, "NotExist should not panic with false")
		assert.Equal(t, "value", result)
	})

	t.Run("True", func(t *testing.T) {
		assert.PanicsWithValue(t, "value should not exist", func() {
			must.NotExist("value", true)
		})
	})
}

func TestTrue(t *testing.T) {
	t.Run("True", func(t *testing.T) {
		assert.NotPanicsf(t, func() {
			must.True(true)
		}, "True should not panic with true value")
	})

	t.Run("False", func(t *testing.T) {
		assert.PanicsWithValue(t, "assertion failed: expected true", func() {
			must.True(false)
		})
	})
}

func TestFalse(t *testing.T) {
	t.Run("False", func(t *testing.T) {
		assert.NotPanicsf(t, func() {
			must.False(false)
		}, "False should not panic with false value")
	})

	t.Run("True", func(t *testing.T) {
		assert.PanicsWithValue(t, "assertion failed: expected false", func() {
			must.False(true)
		})
	})
}
