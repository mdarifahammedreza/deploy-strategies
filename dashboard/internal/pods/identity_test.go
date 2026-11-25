package pods

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fakePodServer pretends to be a backend pod listening on 127.0.0.1:<port>.
func fakePodServer(t *testing.T) (host, port string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"version":"v2","color":"mediumseagreen","pod":"x"}`)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	host, port, _ = net.SplitHostPort(u.Host)
	return host, port
}

func podWithIP(name, ip string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}, Status: corev1.PodStatus{PodIP: ip}}
}

func TestProbeAllRecordsIdentityAndColors(t *testing.T) {
	host, port := fakePodServer(t)
	list := func() ([]*corev1.Pod, error) {
		return []*corev1.Pod{podWithIP("with-ip", host), podWithIP("no-ip", "")}, nil
	}
	ip := NewIdentityProber(list, port, &http.Client{Timeout: time.Second}, time.Second)
	ip.ProbeAll(context.Background())

	ids := ip.Identities()
	if got := ids["with-ip"]; got != (Identity{Version: "v2", Color: "mediumseagreen"}) {
		t.Fatalf("identity = %+v", got)
	}
	if _, ok := ids["no-ip"]; ok {
		t.Fatal("pod without IP must not have an identity")
	}
	if got := ip.Colors()["v2"]; got != "mediumseagreen" {
		t.Fatalf("learned color for v2 = %q", got)
	}
}

func TestProbeAllForgetsDeletedPods(t *testing.T) {
	host, port := fakePodServer(t)
	current := []*corev1.Pod{podWithIP("a", host)}
	list := func() ([]*corev1.Pod, error) { return current, nil }
	ip := NewIdentityProber(list, port, &http.Client{Timeout: time.Second}, time.Second)

	ip.ProbeAll(context.Background())
	current = nil // pod "a" was deleted
	ip.ProbeAll(context.Background())

	if n := len(ip.Identities()); n != 0 {
		t.Fatalf("expected no identities, got %d", n)
	}
	if ip.Colors()["v2"] != "mediumseagreen" {
		t.Fatal("learned colors should be remembered after pods go away")
	}
}
