import type { PodView, Snapshot } from './types';

export interface VersionGroup {
  version: string;
  color: string;
  pods: PodView[];
  trafficPercent: number;
}

const byNaturalOrder = (a: string, b: string) => a.localeCompare(b, undefined, { numeric: true });

// One group (= one panel on screen) per version, e.g. v1 and v2 during a rollout.
export function groupByVersion(snap: Snapshot): VersionGroup[] {
  const traffic = new Map(snap.traffic.byVersion.map((v) => [v.version, v]));
  const groups = new Map<string, VersionGroup>();
  for (const p of snap.pods) {
    let group = groups.get(p.version);
    if (!group) {
      const share = traffic.get(p.version);
      group = { version: p.version, color: share?.color ?? '', pods: [], trafficPercent: share?.percent ?? 0 };
      groups.set(p.version, group);
    }
    if (p.color) group.color = p.color; // a pod's own answer is the most reliable color
    group.pods.push(p);
  }
  return [...groups.values()].sort((a, b) => byNaturalOrder(a.version, b.version));
}
