import { describe, expect, it } from 'vitest';
import { diffEvents } from './events';
import { pod, share, snapshot } from './test-fixtures';

describe('diffEvents', () => {
  it('returns nothing for the first snapshot', () => {
    expect(diffEvents(null, snapshot([pod()]))).toEqual([]);
  });

  it('reports created, changed and removed pods', () => {
    const before = snapshot([
      pod({ name: 'color-backend-old', status: 'Ready' }),
      pod({ name: 'color-backend-mid', status: 'Starting', version: 'v2' }),
    ]);
    const after = snapshot([
      pod({ name: 'color-backend-mid', status: 'Ready', version: 'v2' }),
      pod({ name: 'color-backend-new', status: 'Pending', version: 'v2', node: '' }),
    ]);
    expect(diffEvents(before, after)).toEqual([
      'mid (v2) Starting → Ready',
      '+ new (v2) created on unscheduled',
      '- old (v1) removed',
    ]);
  });

  it('reports when a different version takes most of the traffic', () => {
    const before = snapshot([], [share('v1', 100)]);
    const after = snapshot([], [share('v1', 40), share('v2', 60)]);
    expect(diffEvents(before, after)).toEqual(['traffic: v2 now serves 60%']);
  });
});
