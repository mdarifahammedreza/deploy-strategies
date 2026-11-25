import type { PodView, Snapshot, VersionShare } from './types';

export function pod(over: Partial<PodView> = {}): PodView {
  return {
    name: 'color-backend-abc-1', node: 'worker', ip: '10.0.0.1', version: 'v1', color: 'royalblue',
    status: 'Ready', labels: {}, hits: 0, errors: 0, ageSeconds: 1, ...over,
  };
}

export function snapshot(pods: PodView[], byVersion: VersionShare[] = []): Snapshot {
  const total = byVersion.reduce((n, v) => n + v.count, 0);
  return {
    namespace: 'color-demo', target: 'http://color-backend/api/color', time: '2026-11-25T12:00:00Z',
    pods, traffic: { total, errors: 0, byVersion },
  };
}

export function share(version: string, percent: number, over: Partial<VersionShare> = {}): VersionShare {
  return { version, color: '', count: percent, errors: 0, percent, ...over };
}
