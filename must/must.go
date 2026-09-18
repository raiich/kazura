// Package must provides utility functions for panic-based error handling.
// These functions help eliminate explicit error checking by panicking on failure conditions.
package must

// Must returns v if err is nil, and panics with err otherwise.
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

// NoError panics with err if it is not nil.
func NoError(err error) {
	if err != nil {
		panic(err)
	}
}

// Exist returns v if ok is true, and panics otherwise.
//
// Example:
//
//	v, ok := m["key"]
//	value := must.Exist(v, ok)
func Exist[T any](v T, ok bool) T {
	if !ok {
		panic("value does not exist")
	}
	return v
}

// NotExist returns v if ok is false, and panics otherwise.
func NotExist[T any](v T, ok bool) T {
	if ok {
		panic("value should not exist")
	}
	return v
}

// True panics if v is false.
func True(v bool) {
	if !v {
		panic("assertion failed: expected true")
	}
}

// False panics if v is true.
func False(v bool) {
	if v {
		panic("assertion failed: expected false")
	}
}
