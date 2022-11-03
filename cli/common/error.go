package common

// DanglingResourceError should be used when a request was made to remove
// a resource and the resource still remains after enough time has elapsed
// that it should have been removed by Kubernetes.
type DanglingResourceError struct {
	message string
}

// NewDanglingResourceError returns a new instance of DanglingResourceError with
// the given message.
func NewDanglingResourceError(message string) *DanglingResourceError {
	return &DanglingResourceError{message}
}

// Error returns a string representation of the dangling resource error.
func (d *DanglingResourceError) Error() string {
	return d.message
}

// IsDanglingResourceError returns true if the error passed in is of type DanglingResourceError.
func IsDanglingResourceError(err error) bool {
	_, ok := err.(*DanglingResourceError)
	return ok
}
