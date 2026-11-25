// Mirrors the JSON produced by dashboard/internal/state/snapshot.go.

export type PodStatus = 'Pending' | 'Starting' | 'Ready' | 'Terminating' | 'Failed';

export interface PodView {
  name: string;
  node: string;
  ip: string;
  version: string;
  color: string;
  status: PodStatus;
  labels: Record<string, string>;
  hits: number; // requests answered within the traffic window
  errors: number;
  ageSeconds: number;
}

export interface VersionShare {
  version: string;
  color: string;
  count: number;
  errors: number;
  percent: number;
}

export interface TrafficSummary {
  total: number;
  errors: number;
  byVersion: VersionShare[];
}

export interface Snapshot {
  namespace: string;
  target: string;
  time: string;
  pods: PodView[];
  traffic: TrafficSummary;
}
