import { beforeEach, describe, expect, it } from 'vitest';
import { badgeText, renderGroups, renderTraffic } from './render';
import { groupByVersion } from './group';
import { pod, share, snapshot } from './test-fixtures';

describe('renderGroups', () => {
  let root: HTMLElement;
  beforeEach(() => { root = document.createElement('main'); });

  it('draws one panel per version and one box per pod', () => {
    renderGroups(root, groupByVersion(snapshot([
      pod({ name: 'a', version: 'v1' }), pod({ name: 'b', version: 'v2', status: 'Starting' }),
    ])));
    expect(root.querySelectorAll('.group')).toHaveLength(2);
    const boxes = root.querySelectorAll<HTMLElement>('.pod');
    expect([...boxes].map((b) => b.dataset.status)).toEqual(['Ready', 'Starting']);
  });

  it('updates boxes in place instead of re-creating them', () => {
    renderGroups(root, groupByVersion(snapshot([pod({ name: 'a', status: 'Starting' })])));
    const first = root.querySelector('.pod');
    renderGroups(root, groupByVersion(snapshot([pod({ name: 'a', status: 'Ready' })])));
    const second = root.querySelector<HTMLElement>('.pod');
    expect(second).toBe(first);
    expect(second!.dataset.status).toBe('Ready');
  });

  it('removes boxes of deleted pods', () => {
    renderGroups(root, groupByVersion(snapshot([pod({ name: 'a' }), pod({ name: 'b' })])));
    renderGroups(root, groupByVersion(snapshot([pod({ name: 'b' })])));
    expect(root.querySelectorAll('.pod')).toHaveLength(1);
  });
});

describe('renderTraffic', () => {
  it('sizes one bar segment per version by its percentage', () => {
    const panel = document.createElement('section');
    panel.innerHTML = '<div class="traffic-bar"></div><div class="traffic-legend"></div>';
    renderTraffic(panel, snapshot([], [share('v1', 90), share('v2', 10, { errors: 3 })]).traffic);
    const widths = [...panel.querySelectorAll<HTMLElement>('.segment')].map((s) => s.style.width);
    expect(widths).toEqual(['90%', '10%']);
    expect(panel.querySelector('.legend-item.has-errors')?.textContent).toContain('3 errors');
  });
});

describe('badgeText', () => {
  it('explains the box state in words', () => {
    expect(badgeText(pod({ status: 'Starting' }))).toBe('Starting');
    expect(badgeText(pod({ hits: 0 }))).toBe('no traffic');
    expect(badgeText(pod({ hits: 5, errors: 2 }))).toBe('2 errors');
    expect(badgeText(pod({ hits: 5 }))).toBe('serving');
  });
});
