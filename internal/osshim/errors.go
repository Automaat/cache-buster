package osshim

import "errors"

// ErrOpenFilesUnsupported is returned where open handles cannot be listed.
// The answer is unknown, so a caller either treats it as "open" (the
// directory-pattern provider does) or must have another check that refuses
// the removal: the project-artifacts provider relies on Windows refusing to
// rename a directory that holds open files.
var ErrOpenFilesUnsupported = errors.New("open handles cannot be listed on windows")
