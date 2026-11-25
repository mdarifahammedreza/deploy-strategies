package state

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"deploy-strategies/dashboard/internal/pods"
	"deploy-strategies/dashboard/internal/traffic"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func backendPod(name, image string, age time.Duration, ready bool) *corev1.Pod {
	cond := corev1.ConditionFalse
	if ready {
		cond = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(now.Add(-age)),
			Labels:            map[string]string{"app": "color-backend", "pod-template-hash": "abc", "slot": "blue"},
		},
		Spec: corev1.PodSpec{NodeName: "worker", Containers: []corev1.Container{{Name: "app", Image: image}}},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			PodIP:      "10.0.0.1",
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: cond}},
		},
	}
}

func TestBuildMergesPodsIdentitiesAndTraffic(t *testing.T) {
	snap := Build(Input{
		Namespace: "color-demo",
		Target:    "http://color-backend/api/color",
		Now:       now,
		Pods: []*corev1.Pod{
			backendPod("color-backend-b", "color-backend:v2", 10*time.Second, false), // new, not answering yet
			backendPod("color-backend-a", "color-backend:v1", 60*time.Second, true),
		},
		Identities: map[string]pods.Identity{"color-backend-a": {Version: "v1", Color: "royalblue"}},
		Colors:     map[string]string{"v1": "royalblue"},
		Traffic: traffic.Summary{
			Total: 4,
			ByVersion: []traffic.VersionShare{
				{Version: "v1", Color: "royalblue", Count: 2, Percent: 50},
				{Version: "v2", Color: "mediumseagreen", Count: 2, Percent: 50},
			},
			ByPod: map[string]traffic.PodCount{"color-backend-a": {Hits: 2}},
		},
	})

	if len(snap.Pods) != 2 {
		t.Fatalf("want 2 pods, got %d", len(snap.Pods))
	}
	a, b := snap.Pods[0], snap.Pods[1] // sorted by version: v1 first
	if a.Name != "color-backend-a" || a.Status != pods.StatusReady || a.Hits != 2 ||
		a.Color != "royalblue" || a.AgeSeconds != 60 || a.Node != "worker" {
		t.Fatalf("pod a = %+v", a)
	}
	if _, has := a.Labels["pod-template-hash"]; has || a.Labels["slot"] != "blue" {
		t.Fatalf("labels = %v", a.Labels)
	}
	if b.Version != "v2" || b.Color != "mediumseagreen" || b.Status != pods.StatusStarting {
		t.Fatalf("pod b = %+v (version from image, color learned from traffic)", b)
	}
	if snap.Namespace != "color-demo" || snap.Traffic.Total != 4 {
		t.Fatalf("snapshot header = %+v", snap)
	}
}

func TestBuildNeverReturnsNilSlices(t *testing.T) {
	snap := Build(Input{Now: now})
	if snap.Pods == nil || snap.Traffic.ByVersion == nil {
		t.Fatalf("nil slice would become JSON null: %+v", snap)
	}
}
