package eventloop

import "time"

// timeNow returns a fixed time for consistent testing
func timeNow() time.Time {
	return time.Unix(0, 0)
}
