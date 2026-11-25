// Package traffic measures which backend versions really answer requests sent through the Service.
package traffic

import (
	"math"
	"sort"
	"sync"
)

// Unreachable is the pseudo-version recorded when no pod answered at all.
const Unreachable = "unreachable"

// Result is the outcome of one request sent through the Service.
type Result struct {
	Pod     string
	Version string
	Color   string
	OK      bool // false for HTTP errors and connection failures
}

// VersionShare says how much of the recent traffic one version answered.
type VersionShare struct {
	Version string  `json:"version"`
	Color   string  `json:"color"`
	Count   int     `json:"count"`
	Errors  int     `json:"errors"`
	Percent float64 `json:"percent"`
}

// PodCount is how many of the recent requests one pod answered.
type PodCount struct {
	Hits   int
	Errors int
}

// Summary aggregates the window.
type Summary struct {
	Total     int                 `json:"total"`
	Errors    int                 `json:"errors"`
	ByVersion []VersionShare      `json:"byVersion"`
	ByPod     map[string]PodCount `json:"-"`
}

// Window is a fixed-size ring buffer holding the last N results. Safe for concurrent use.
type Window struct {
	mu   sync.Mutex
	buf  []Result
	next int  // where the next result is written
	full bool // true once we have wrapped around at least once
}

func NewWindow(size int) *Window {
	return &Window{buf: make([]Result, size)}
}

// Add stores a result, overwriting the oldest one when the buffer is full.
func (w *Window) Add(r Result) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf[w.next] = r
	w.next = (w.next + 1) % len(w.buf)
	if w.next == 0 {
		w.full = true
	}
}

// Summary counts the results currently in the window.
func (w *Window) Summary() Summary {
	w.mu.Lock()
	n := w.next
	if w.full {
		n = len(w.buf)
	}
	items := make([]Result, n)
	copy(items, w.buf[:n])
	w.mu.Unlock()

	s := Summary{ByVersion: []VersionShare{}, ByPod: map[string]PodCount{}}
	index := map[string]int{} // version → position in s.ByVersion
	for _, r := range items {
		s.Total++
		i, seen := index[r.Version]
		if !seen {
			i = len(s.ByVersion)
			index[r.Version] = i
			s.ByVersion = append(s.ByVersion, VersionShare{Version: r.Version})
		}
		share := &s.ByVersion[i]
		share.Count++
		if share.Color == "" {
			share.Color = r.Color
		}
		pod := s.ByPod[r.Pod]
		pod.Hits++
		if !r.OK {
			s.Errors++
			share.Errors++
			pod.Errors++
		}
		if r.Pod != "" {
			s.ByPod[r.Pod] = pod
		}
	}
	for i := range s.ByVersion {
		pct := float64(s.ByVersion[i].Count) * 100 / float64(s.Total)
		s.ByVersion[i].Percent = math.Round(pct*10) / 10 // one decimal place
	}
	sort.Slice(s.ByVersion, func(a, b int) bool { return s.ByVersion[a].Version < s.ByVersion[b].Version })
	return s
}
