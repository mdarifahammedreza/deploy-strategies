import { describe, expect, it } from 'vitest';
import { groupByVersion } from './group';
import { pod, share, snapshot } from './test-fixtures';

describe('groupByVersion', () => {
  it('groups pods by version in natural order with traffic share', () => {
    const groups = groupByVersion(snapshot(
      [pod({ name: 'a', version: 'v10' }), pod({ name: 'b', version: 'v2' }), pod({ name: 'c', version: 'v2' })],
      [share('v2', 75), share('v10', 25)],
    ));
    expect(groups.map((g) => g.version)).toEqual(['v2', 'v10']);
    expect(groups[0].pods.map((p) => p.name)).toEqual(['b', 'c']);
    expect(groups[0].trafficPercent).toBe(75);
  });

  it('takes the color from traffic when no pod knows it yet', () => {
    const groups = groupByVersion(snapshot(
      [pod({ version: 'v2', color: '', status: 'Pending' })],
      [share('v2', 100, { color: 'mediumseagreen' })],
    ));
    expect(groups[0].color).toBe('mediumseagreen');
  });

  it('reports 0% for a version that gets no traffic', () => {
    expect(groupByVersion(snapshot([pod({ version: 'v2' })]))[0].trafficPercent).toBe(0);
  });
});
