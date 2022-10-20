package common

// DeletionError should be used when a request was made to delete a resource
// and that request failed.
type DeletionError struct {
	message string
}

// NewDeletionError returns a new instance of DeletionError for handling
// failures in deletion requests.
func NewDeletionError(message string) *DeletionError {
	return &DeletionError{message}
}

// Error returns a string representation of the deletion error.
func (d *DeletionError) Error() string {
	return d.message
}

// IsDeletionError returns true if the error passed in is of type DeletionError.
func IsDeletionError(err error) bool {
	_, ok := err.(*DeletionError)
	return ok
}
