package cache

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestClassifyError_Nil(t *testing.T) {
	result := ClassifyError("/path", nil)
	if result.Reason != ReasonUnknown {
		t.Errorf("expected %q, got %q", ReasonUnknown, result.Reason)
	}
}

func TestClassifyError_PermissionDenied(t *testing.T) {
	result := ClassifyError("/path", os.ErrPermission)
	if result.Reason != ReasonPermissionDenied {
		t.Errorf("expected %q, got %q", ReasonPermissionDenied, result.Reason)
	}
}

func TestClassifyError_NotExist(t *testing.T) {
	result := ClassifyError("/path", os.ErrNotExist)
	if result.Reason != ReasonNotFound {
		t.Errorf("expected %q, got %q", ReasonNotFound, result.Reason)
	}
}

func TestClassifyError_EBUSY(t *testing.T) {
	result := ClassifyError("/path", syscall.EBUSY)
	if result.Reason != ReasonFileLocked {
		t.Errorf("expected %q, got %q", ReasonFileLocked, result.Reason)
	}
}

func TestClassifyError_ETXTBSY(t *testing.T) {
	result := ClassifyError("/path", syscall.ETXTBSY)
	if result.Reason != ReasonFileLocked {
		t.Errorf("expected %q, got %q", ReasonFileLocked, result.Reason)
	}
}

func TestClassifyError_Unknown(t *testing.T) {
	result := ClassifyError("/path", errors.New("custom error"))
	if result.Reason != ReasonUnknown {
		t.Errorf("expected %q, got %q", ReasonUnknown, result.Reason)
	}
}

func TestAccessError_Error(t *testing.T) {
	err := AccessError{Path: "/test/path", Reason: ReasonPermissionDenied}
	expected := "/test/path: permission denied"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestAccessError_Unwrap(t *testing.T) {
	original := os.ErrPermission
	err := ClassifyError("/path", original)
	if !errors.Is(err, original) {
		t.Error("Unwrap should return original error")
	}
}
