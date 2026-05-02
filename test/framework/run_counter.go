package framework

// RunCounter is implemented by VM backends that track how many times Run()
// has been called. Assertions use this interface to count script invocations
// without depending on a concrete VM type.
//
// DockerVM implements RunCounter, so all run-count assertions work correctly
// in container mode.
type RunCounter interface {
	RunCount() int
	ResetRunCount()
}
