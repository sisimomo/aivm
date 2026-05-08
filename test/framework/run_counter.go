package framework

// RunCounter is implemented by VM wrappers that track how many times Run()
// has been called. Assertions use this interface to count script invocations
// without depending on a concrete VM type.
//
// RunTrackingVM implements RunCounter and wraps any vm.VM. The test harness
// uses it so run-count assertions work correctly in container mode.
type RunCounter interface {
	RunCount() int
	ResetRunCount()
}
