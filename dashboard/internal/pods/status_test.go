package pods

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makePod(mod func(*corev1.Pod)) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "color-backend:v1"}}},
	}
	mod(p)
	return p
}

func readyCondition(status corev1.ConditionStatus) []corev1.PodCondition {
	return []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}
}

func TestStatus(t *testing.T) {
	now := metav1.Now()
	cases := []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{"pending", makePod(func(p *corev1.Pod) { p.Status.Phase = corev1.PodPending }), StatusPending},
		{"running not ready", makePod(func(p *corev1.Pod) {
			p.Status.Phase = corev1.PodRunning
			p.Status.Conditions = readyCondition(corev1.ConditionFalse)
		}), StatusStarting},
		{"ready", makePod(func(p *corev1.Pod) {
			p.Status.Phase = corev1.PodRunning
			p.Status.Conditions = readyCondition(corev1.ConditionTrue)
		}), StatusReady},
		{"terminating wins over ready", makePod(func(p *corev1.Pod) {
			p.Status.Phase = corev1.PodRunning
			p.Status.Conditions = readyCondition(corev1.ConditionTrue)
			p.DeletionTimestamp = &now
		}), StatusTerminating},
		{"failed", makePod(func(p *corev1.Pod) { p.Status.Phase = corev1.PodFailed }), StatusFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Status(c.pod); got != c.want {
				t.Fatalf("Status() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestVersionFromImage(t *testing.T) {
	cases := map[string]string{
		"color-backend:v2":                "v2",
		"localhost:5000/color-backend:v3": "v3",
		"localhost:5000/color-backend":    "latest",
		"color-backend:v1@sha256:abc":     "v1",
		"color-backend":                   "latest",
	}
	for image, want := range cases {
		p := makePod(func(p *corev1.Pod) { p.Spec.Containers[0].Image = image })
		if got := VersionFromImage(p); got != want {
			t.Errorf("VersionFromImage(%q) = %q, want %q", image, got, want)
		}
	}
}
