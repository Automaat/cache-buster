// Package osshim isolates the operating-system specifics the providers need:
// advisory file locks, process listing, process-tree kill, open-file checks
// and free-space queries. A check that cannot run returns an error, and
// callers must treat that as busy so a directory is skipped, never deleted.
package osshim
