package osshim

import "errors"

// ErrOpenFilesUnsupported is returned where open handles cannot be listed;
// callers treat it as "open".
var ErrOpenFilesUnsupported = errors.New("open handles cannot be listed on windows")
