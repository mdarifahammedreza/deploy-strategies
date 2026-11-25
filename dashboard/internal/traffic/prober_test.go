package traffic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func backend(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

func TestProbeOnceReadsVersionFromBody(t *testing.T) {
	srv := backend(200, `{"version":"v2","color":"mediumseagreen","pod":"pod-x"}`)
	defer srv.Close()

	p := &Prober{Target: srv.URL, Client: NewClient(time.Second)}
	got := p.ProbeOnce(context.Background())
	want := Result{Pod: "pod-x", Version: "v2", Color: "mediumseagreen", OK: true}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestProbeOnceMarksServerErrorsAsNotOK(t *testing.T) {
	srv := backend(500, `{"version":"v3","color":"darkorange","pod":"pod-y","error":"simulated failure"}`)
	defer srv.Close()

	got := (&Prober{Target: srv.URL, Client: NewClient(time.Second)}).ProbeOnce(context.Background())
	if got.OK || got.Version != "v3" || got.Pod != "pod-y" {
		t.Fatalf("got %+v", got)
	}
}

func TestProbeOnceReportsUnreachable(t *testing.T) {
	srv := backend(200, `{}`)
	url := srv.URL
	srv.Close() // nothing listens any more

	got := (&Prober{Target: url, Client: NewClient(200 * time.Millisecond)}).ProbeOnce(context.Background())
	if got.OK || got.Version != Unreachable {
		t.Fatalf("got %+v", got)
	}
}

func TestRunFillsWindow(t *testing.T) {
	srv := backend(200, `{"version":"v1","color":"royalblue","pod":"p"}`)
	defer srv.Close()

	w := NewWindow(50)
	p := &Prober{Target: srv.URL, RPS: 100, Window: w, Client: NewClient(time.Second)}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	p.Run(ctx)
	time.Sleep(50 * time.Millisecond) // let in-flight probes finish

	if total := w.Summary().Total; total < 5 {
		t.Fatalf("expected the window to fill up, total=%d", total)
	}
}
