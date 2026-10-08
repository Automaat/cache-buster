package osshim

import "sync"

// FileID identifies one file on one volume, so hard links to the same data
// compare equal.
type FileID struct {
	Volume uint64
	Index  uint64
}

// LinkSet remembers which hard-linked files were already counted. The zero
// value is ready to use and safe for concurrent use.
type LinkSet struct {
	seen map[FileID]struct{}
	mu   sync.Mutex
}

// Add records id and reports whether it was new.
func (s *LinkSet) Add(id FileID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = make(map[FileID]struct{})
	}
	if _, dup := s.seen[id]; dup {
		return false
	}
	s.seen[id] = struct{}{}
	return true
}
