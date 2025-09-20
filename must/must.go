// Package must provides utility functions for panic-based error handling.
// These functions help eliminate explicit error checking by panicking on failure conditions.
package must

// Must returns the value v if err is nil, otherwise panics with the error.
// This is useful for eliminating error checks in situations where errors should be fatal.
//
// Example:
//
//	data := must.Must(os.ReadFile("config.json"))
func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// NoError panics if err is not nil.
// This is useful for asserting that an error should never occur.
func NoError(err error) {
	if err != nil {
		panic(err)
	}
}

// Exist returns the value v if ok is true, otherwise panics.
// This is useful for map lookups or channel receives where the value must exist.
//
// Example:
//
//	value := must.Exist(m["key"])
func Exist[T any](v T, ok bool) T {
	if !ok {
		panic("value does not exist")
	}
	return v
}

// NotExist returns the value v if ok is false, otherwise panics.
// This is useful when you expect a value to not exist.
func NotExist[T any](v T, ok bool) T {
	if ok {
		panic("value should not exist")
	}
	return v
}

// True panics if v is false.
// This is useful for asserting that a condition must be true.
func True(v bool) {
	if !v {
		panic("assertion failed: expected true")
	}
}

// False panics if v is true.
// This is useful for asserting that a condition must be false.
func False(v bool) {
	if v {
		panic("assertion failed: expected false")
	}
}
