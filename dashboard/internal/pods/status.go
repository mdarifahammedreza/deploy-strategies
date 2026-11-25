// Package pods turns Kubernetes Pod objects into what the dashboard draws.
package pods

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// The five box states shown on the dashboard.
const (
	StatusPending     = "Pending"     // accepted, waiting for a node / image / container start
	StatusStarting    = "Starting"    // container running but readiness probe not passing yet
	StatusReady       = "Ready"       // receives traffic from Services
	StatusTerminating = "Terminating" // being shut down (deletionTimestamp is set)
	StatusFailed      = "Failed"      // container exited with an error
)

// Status maps a Pod to one of the five box states.
func Status(p *corev1.Pod) string {
	switch {
	case p.DeletionTimestamp != nil:
		return StatusTerminating
	case p.Status.Phase == corev1.PodFailed:
		return StatusFailed
	case isReady(p):
		return StatusReady
	case p.Status.Phase == corev1.PodRunning:
		return StatusStarting
	default:
		return StatusPending
	}
}

func isReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// VersionFromImage returns the tag of the pod's first container image:
// "color-backend:v2" → "v2". Known even while the pod is still Pending.
func VersionFromImage(p *corev1.Pod) string {
	if len(p.Spec.Containers) == 0 {
		return "unknown"
	}
	image := p.Spec.Containers[0].Image
	if at := strings.Index(image, "@"); at >= 0 { // drop "@sha256:..." digests
		image = image[:at]
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon > slash { // a colon after the last slash is a tag, not a registry port
		return image[colon+1:]
	}
	return "latest"
}
