import { shortName } from './format';
import type { Snapshot, VersionShare } from './types';

function leader(snap: Snapshot): VersionShare | undefined {
  return [...snap.traffic.byVersion].sort((a, b) => b.percent - a.percent)[0];
}

// Compare two snapshots and describe what changed, in the order a student would notice it.
export function diffEvents(prev: Snapshot | null, next: Snapshot): string[] {
  if (!prev) return [];
  const events: string[] = [];
  const before = new Map(prev.pods.map((p) => [p.name, p]));

  for (const p of next.pods) {
    const old = before.get(p.name);
    if (!old) {
      events.push(`+ ${shortName(p.name)} (${p.version}) created on ${p.node || 'unscheduled'}`);
    } else if (old.status !== p.status) {
      events.push(`${shortName(p.name)} (${p.version}) ${old.status} → ${p.status}`);
    }
    before.delete(p.name);
  }
  for (const gone of before.values()) {
    events.push(`- ${shortName(gone.name)} (${gone.version}) removed`);
  }

  const was = leader(prev);
  const now = leader(next);
  if (now && now.percent >= 50 && was?.version !== now.version) {
    events.push(`traffic: ${now.version} now serves ${Math.round(now.percent)}%`);
  }
  return events;
}
