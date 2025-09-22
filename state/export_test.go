// Export guts for testing.

package state

// ActiveTimerCount returns the number of currently active timers.
// This is primarily useful for testing and debugging.
func (m *Manager[S]) ActiveTimerCount() int {
	return len(m.timers.timers)
}

type MachineAccessor[T any] = machineAccessor[T]

func (m *EntryMachine[T]) AsMachineAccessor() *MachineAccessor[T] {
	return (*MachineAccessor[T])(m)
}

func (m *AfterFuncMachine[T]) AsMachineAccessor() *MachineAccessor[T] {
	return (*MachineAccessor[T])(m)
}

func (m *AfterEntryMachine[T]) AsMachineAccessor() *MachineAccessor[T] {
	return (*MachineAccessor[T])(m)
}

func (m *ExitMachine[T]) AsMachineAccessor() *MachineAccessor[T] {
	return (*MachineAccessor[T])(m)
}

func (m *machineAccessor[T]) AsEntryMachine() *EntryMachine[T] {
	return (*EntryMachine[T])(m)
}

func (m *machineAccessor[T]) AsAfterFuncMachine() *AfterFuncMachine[T] {
	return (*AfterFuncMachine[T])(m)
}

func (m *machineAccessor[T]) AsAfterEntryMachine() *AfterEntryMachine[T] {
	return (*AfterEntryMachine[T])(m)
}

func (m *machineAccessor[T]) AsExitMachine() *ExitMachine[T] {
	return (*ExitMachine[T])(m)
}
