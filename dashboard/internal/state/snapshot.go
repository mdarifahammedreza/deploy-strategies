// Package state combines pods, identities and traffic into the JSON the browser draws.
package state

import (
	"maps"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"

	"deploy-strategies/dashboard/internal/pods"
	"deploy-strategies/dashboard/internal/traffic"
)

// PodView is one box on the dashboard.
type PodView struct {
	Name       string            `json:"name"`
	Node       string            `json:"node"`
	IP         string            `json:"ip"`
	Version    string            `json:"version"`
	Color      string            `json:"color"`
	Status     string            `json:"status"`
	Labels     map[string]string `json:"labels"`
	Hits       int               `json:"hits"`
	Errors     int               `json:"errors"`
	AgeSeconds int               `json:"ageSeconds"`

	created time.Time // for sorting only, not sent to the browser
}

// Snapshot is the full message pushed to the browser every tick.
type Snapshot struct {
	Namespace string          `json:"namespace"`
	Target    string          `json:"target"`
	Time      time.Time       `json:"time"`
	Pods      []PodView       `json:"pods"`
	Traffic   traffic.Summary `json:"traffic"`
}

// Input is everything Build needs. Keeping it a plain struct makes Build a pure, testable function.
type Input struct {
	Namespace  string
	Target     string
	Now        time.Time
	Pods       []*corev1.Pod
	Identities map[string]pods.Identity
	Colors     map[string]string
	Traffic    traffic.Summary
}

// Build merges the inputs into one Snapshot.
func Build(in Input) Snapshot {
	// Colors per version: learned from direct pod probes first, then from traffic answers.
	colors := maps.Clone(in.Colors)
	if colors == nil {
		colors = map[string]string{}
	}
	for _, share := range in.Traffic.ByVersion {
		if colors[share.Version] == "" && share.Color != "" {
			colors[share.Version] = share.Color
		}
	}

	views := make([]PodView, 0, len(in.Pods))
	for _, p := range in.Pods {
		version := pods.VersionFromImage(p) // known even while Pending
		color := colors[version]
		if id, ok := in.Identities[p.Name]; ok { // the pod's own answer wins
			version, color = id.Version, id.Color
		}

		labels := maps.Clone(p.Labels)
		if labels == nil {
			labels = map[string]string{}
		}
		delete(labels, "pod-template-hash") // noise for students

		count := in.Traffic.ByPod[p.Name]
		views = append(views, PodView{
			Name:       p.Name,
			Node:       p.Spec.NodeName,
			IP:         p.Status.PodIP,
			Version:    version,
			Color:      color,
			Status:     pods.Status(p),
			Labels:     labels,
			Hits:       count.Hits,
			Errors:     count.Errors,
			AgeSeconds: int(in.Now.Sub(p.CreationTimestamp.Time).Seconds()),
			created:    p.CreationTimestamp.Time,
		})
	}
	// Stable order so boxes don't jump around: version, then age (oldest first), then name.
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		if !a.created.Equal(b.created) {
			return a.created.Before(b.created)
		}
		return a.Name < b.Name
	})

	summary := in.Traffic
	if summary.ByVersion == nil {
		summary.ByVersion = []traffic.VersionShare{}
	}
	return Snapshot{Namespace: in.Namespace, Target: in.Target, Time: in.Now.UTC(), Pods: views, Traffic: summary}
}
