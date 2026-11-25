import { shortName } from './format';
import type { VersionGroup } from './group';
import type { PodView, TrafficSummary, VersionShare } from './types';

export const FALLBACK_COLOR = '#9ca3af';
const UNREACHABLE_COLOR = '#1f2937';

function el(tag: string, className: string): HTMLElement {
  const node = document.createElement(tag);
  node.className = className;
  return node;
}

// Keyed update: reuse the DOM element of an item that already exists, create new ones, remove stale ones.
// Re-using elements keeps CSS animations smooth instead of restarting them every 500 ms.
export function syncChildren<T>(
  parent: Element,
  items: T[],
  key: (item: T) => string,
  create: (item: T) => HTMLElement,
  update: (node: HTMLElement, item: T) => void,
): void {
  const existing = new Map<string, HTMLElement>();
  for (const child of Array.from(parent.children) as HTMLElement[]) {
    if (child.dataset.key !== undefined) existing.set(child.dataset.key, child);
  }
  items.forEach((item, index) => {
    const k = key(item);
    let node = existing.get(k);
    if (node) {
      existing.delete(k);
    } else {
      node = create(item);
      node.dataset.key = k;
    }
    update(node, item);
    const current = parent.children[index];
    if (current !== node) parent.insertBefore(node, current ?? null);
  });
  for (const stale of existing.values()) stale.remove();
}

function shareColor(v: VersionShare): string {
  return v.version === 'unreachable' ? UNREACHABLE_COLOR : v.color || FALLBACK_COLOR;
}

export function renderTraffic(panel: Element, traffic: TrafficSummary): void {
  const bar = panel.querySelector('.traffic-bar')!;
  const legend = panel.querySelector('.traffic-legend')!;

  syncChildren(bar, traffic.byVersion, (v) => v.version, () => el('div', 'segment'), (seg, v) => {
    seg.style.width = `${v.percent}%`;
    seg.style.background = shareColor(v);
    seg.textContent = v.percent >= 8 ? `${v.version} ${Math.round(v.percent)}%` : '';
  });

  syncChildren(
    legend,
    traffic.byVersion,
    (v) => v.version,
    () => {
      const item = el('span', 'legend-item');
      item.append(el('span', 'swatch'), el('span', 'legend-text'));
      return item;
    },
    (item, v) => {
      item.classList.toggle('has-errors', v.errors > 0);
      item.querySelector<HTMLElement>('.swatch')!.style.setProperty('--swatch', shareColor(v));
      const errors = v.errors > 0 ? `, ${v.errors} errors` : '';
      item.querySelector('.legend-text')!.textContent = `${v.version}: ${v.percent.toFixed(1)}% (${v.count} req${errors})`;
    },
  );
}

export function badgeText(p: PodView): string {
  if (p.status !== 'Ready') return p.status;
  if (p.errors > 0) return `${p.errors} errors`;
  if (p.hits === 0) return 'no traffic';
  return 'serving';
}

function createPod(): HTMLElement {
  const box = el('div', 'pod');
  box.append(el('span', 'pod-badge'), el('span', 'pod-name'), el('span', 'pod-meta'));
  return box;
}

function updatePod(box: HTMLElement, p: PodView): void {
  box.dataset.status = p.status;
  box.style.setProperty('--pod-color', p.color || FALLBACK_COLOR);
  box.classList.toggle('idle', p.status === 'Ready' && p.hits === 0);
  box.classList.toggle('erroring', p.errors > 0);
  box.title = `${p.name}\nversion: ${p.version}\nnode: ${p.node || '-'}\nip: ${p.ip || '-'}`;
  box.querySelector('.pod-badge')!.textContent = badgeText(p);
  box.querySelector('.pod-name')!.textContent = shortName(p.name);
  const tags = Object.entries(p.labels)
    .filter(([k]) => k === 'slot' || k === 'track')
    .map(([k, v]) => `${k}=${v}`);
  box.querySelector('.pod-meta')!.textContent = [p.node || 'unscheduled', `${p.hits} req`, ...tags].join(' · ');
}

function createGroup(): HTMLElement {
  const section = el('section', 'panel group');
  section.append(el('h2', 'group-title'), el('div', 'grid'));
  return section;
}

function updateGroup(section: HTMLElement, g: VersionGroup): void {
  section.style.setProperty('--group-color', g.color || FALLBACK_COLOR);
  const ready = g.pods.filter((p) => p.status === 'Ready').length;
  section.querySelector('.group-title')!.textContent =
    `${g.version} · ${ready}/${g.pods.length} ready · ${Math.round(g.trafficPercent)}% of traffic`;
  syncChildren(section.querySelector('.grid')!, g.pods, (p) => p.name, createPod, updatePod);
}

export function renderGroups(container: Element, groups: VersionGroup[]): void {
  syncChildren(container, groups, (g) => g.version, createGroup, updateGroup);
}

export function renderEvents(list: Element, lines: string[]): void {
  list.replaceChildren(...lines.map((line) => {
    const li = document.createElement('li');
    li.textContent = line;
    return li;
  }));
}
