package traffic

import "testing"

func TestSummaryCountsVersionsAndErrors(t *testing.T) {
	w := NewWindow(10)
	for i := 0; i < 6; i++ {
		w.Add(Result{Pod: "a", Version: "v1", Color: "royalblue", OK: true})
	}
	for i := 0; i < 3; i++ {
		w.Add(Result{Pod: "b", Version: "v2", Color: "mediumseagreen", OK: true})
	}
	w.Add(Result{Pod: "b", Version: "v2", Color: "mediumseagreen", OK: false})

	s := w.Summary()
	if s.Total != 10 || s.Errors != 1 {
		t.Fatalf("total=%d errors=%d, want 10 and 1", s.Total, s.Errors)
	}
	if len(s.ByVersion) != 2 {
		t.Fatalf("want 2 versions, got %+v", s.ByVersion)
	}
	v1, v2 := s.ByVersion[0], s.ByVersion[1]
	if v1.Version != "v1" || v1.Count != 6 || v1.Percent != 60 || v1.Color != "royalblue" {
		t.Fatalf("bad v1 share %+v", v1)
	}
	if v2.Version != "v2" || v2.Count != 4 || v2.Errors != 1 || v2.Percent != 40 {
		t.Fatalf("bad v2 share %+v", v2)
	}
	if got := s.ByPod["b"]; got != (PodCount{Hits: 4, Errors: 1}) {
		t.Fatalf("pod b = %+v", got)
	}
}

func TestWindowKeepsOnlyLastN(t *testing.T) {
	w := NewWindow(3)
	for i := 0; i < 3; i++ {
		w.Add(Result{Version: "v1", OK: true})
	}
	w.Add(Result{Version: "v2", OK: true})
	w.Add(Result{Version: "v2", OK: true})

	s := w.Summary()
	if s.Total != 3 || s.ByVersion[0].Count != 1 || s.ByVersion[1].Count != 2 {
		t.Fatalf("ring buffer did not drop old results: %+v", s)
	}
}

func TestSummaryOfEmptyWindowHasNoNils(t *testing.T) {
	s := NewWindow(5).Summary()
	if s.Total != 0 || s.ByVersion == nil || s.ByPod == nil {
		t.Fatalf("empty summary must have non-nil collections: %+v", s)
	}
}
