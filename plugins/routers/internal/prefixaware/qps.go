package prefixaware

import (
	"sync"
	"time"
)

const qpsWindow = 60 * time.Second

type qpsTracker struct {
	mu     sync.Mutex
	events map[string][]time.Time
	now    func() time.Time
}

func newQPSTracker() *qpsTracker {
	return &qpsTracker{events: make(map[string][]time.Time), now: time.Now}
}

func (q *qpsTracker) selectLowest(names []string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now()
	selected := ""
	lowest := int(^uint(0) >> 1)
	for _, name := range names {
		events, seen := q.events[name]
		if !seen {
			return name
		}
		cutoff := now.Add(-qpsWindow)
		first := 0
		for first < len(events) && events[first].Before(cutoff) {
			first++
		}
		events = events[first:]
		q.events[name] = events
		if len(events) < lowest {
			selected, lowest = name, len(events)
		}
	}
	return selected
}

func (q *qpsTracker) record(name string) {
	if name == "" {
		return
	}
	q.mu.Lock()
	q.events[name] = append(q.events[name], q.now())
	q.mu.Unlock()
}
