package pausable

// TrackedCount returns the number of tracked entries for testing.
func (d *Dispatcher) TrackedCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.tracked)
}
