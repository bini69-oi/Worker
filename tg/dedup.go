package tg

import "sync"

// DedupSize это сколько последних update_id помним.
const DedupSize = 10000

// Dedup это кольцо последних update_id. Безопасен для вызова из нескольких горутин.
type Dedup struct {
	mu   sync.Mutex
	ring []int64
	set  map[int64]struct{}
	next int
}

// NewDedup создаёт кольцо на size id (не больше нуля заменяется на DedupSize).
func NewDedup(size int) *Dedup {
	if size <= 0 {
		size = DedupSize
	}
	return &Dedup{ring: make([]int64, 0, size), set: make(map[int64]struct{}, size)}
}

// Seen говорит, видели ли мы id раньше, и запоминает его.
func (d *Dedup) Seen(id int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.set[id]; ok {
		return true
	}
	if len(d.ring) < cap(d.ring) {
		d.ring = append(d.ring, id)
	} else {
		delete(d.set, d.ring[d.next])
		d.ring[d.next] = id
		d.next = (d.next + 1) % len(d.ring)
	}
	d.set[id] = struct{}{}
	return false
}
