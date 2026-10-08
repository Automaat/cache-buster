package osshim

// procInfo is one row of a process snapshot.
type procInfo struct {
	pid  uint32
	ppid uint32
}

// descendants returns every transitive child of root in the snapshot, deepest
// first, so killing in order never orphans a victim to a surviving parent.
// root itself is not included.
func descendants(root uint32, procs []procInfo) []uint32 {
	children := make(map[uint32][]uint32, len(procs))
	for _, p := range procs {
		if p.pid != p.ppid {
			children[p.ppid] = append(children[p.ppid], p.pid)
		}
	}

	seen := map[uint32]bool{root: true}
	order := []uint32{}
	queue := []uint32{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			if seen[c] {
				continue
			}
			seen[c] = true
			order = append(order, c)
			queue = append(queue, c)
		}
	}

	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}
	return order
}
