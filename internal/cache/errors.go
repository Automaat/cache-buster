package cache

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// AccessError represents a file access error with classification.
type AccessError struct {
	Err    error
	Path   string
	Reason string
}

func (e AccessError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Reason)
}

func (e AccessError) Unwrap() error {
	return e.Err
}

// Reason constants for error classification.
const (
	ReasonPermissionDenied = "permission denied"
	ReasonFileLocked       = "file locked"
	ReasonNotFound         = "not found"
	ReasonUnknown          = "access error"
)

// ClassifyError determines the reason for a file access error.
func ClassifyError(path string, err error) AccessError {
	if err == nil {
		return AccessError{Path: path, Reason: ReasonUnknown}
	}

	reason := ReasonUnknown

	switch {
	case errors.Is(err, os.ErrPermission):
		reason = ReasonPermissionDenied
	case errors.Is(err, os.ErrNotExist):
		reason = ReasonNotFound
	case errors.Is(err, syscall.EBUSY):
		reason = ReasonFileLocked
	case errors.Is(err, syscall.ETXTBSY):
		reason = ReasonFileLocked
	}

	return AccessError{
		Path:   path,
		Reason: reason,
		Err:    err,
	}
}
