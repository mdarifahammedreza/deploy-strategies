package hub

import (
	"testing"
	"time"
)

func receive(t *testing.T, ch <-chan []byte) string {
	t.Helper()
	select {
	case msg := <-ch:
		return string(msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a message")
		return ""
	}
}

func TestSubscriberGetsLatestImmediately(t *testing.T) {
	h := New()
	h.Publish([]byte("one"))
	ch, cancel := h.Subscribe()
	defer cancel()
	if got := receive(t, ch); got != "one" {
		t.Fatalf("got %q", got)
	}
}

func TestSlowSubscriberOnlyGetsNewest(t *testing.T) {
	h := New()
	ch, cancel := h.Subscribe()
	defer cancel()
	h.Publish([]byte("a"))
	h.Publish([]byte("b"))
	h.Publish([]byte("c"))
	if got := receive(t, ch); got != "c" {
		t.Fatalf("got %q, want newest message c", got)
	}
	select {
	case extra := <-ch:
		t.Fatalf("unexpected extra message %q", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCancelUnsubscribes(t *testing.T) {
	h := New()
	_, cancel := h.Subscribe()
	cancel()
	if n := h.Subscribers(); n != 0 {
		t.Fatalf("subscribers = %d, want 0", n)
	}
	h.Publish([]byte("x")) // must not block or panic
	if string(h.Latest()) != "x" {
		t.Fatal("Latest() should return the last published message")
	}
}
