# Walkthrough 3: The Frontend (Vite + TypeScript)

The frontend is the web page students see. It opens a WebSocket to the dashboard, receives a snapshot twice per second, and draws boxes. There is **no framework** (no React, no Vue): just TypeScript and plain DOM calls. That keeps the code small enough to read in one lesson.

Quick TypeScript notes for beginners:
- TypeScript is JavaScript plus **types**. `name: string` says "name must be text". Types are removed when the code is built, so they cost nothing at runtime.
- `interface` describes the shape of an object. `type X = 'a' | 'b'` means "X is either the text a or the text b".
- `const` is a variable that is not reassigned. `let` can be reassigned.
- `x?.y` means "y of x, but undefined if x is missing". `a ?? b` means "a, unless a is null/undefined, then b".
- `!` after a value (like `querySelector(...)!`) tells TypeScript "I promise this is not null".
- The **DOM** is the tree of HTML elements the browser keeps in memory. We change the page by changing this tree.

| File | Job |
|---|---|
| `index.html` | The empty page shell |
| `vite.config.ts` | Dev server and test settings |
| `src/types.ts` | Types of the snapshot JSON |
| `src/format.ts` | Short pod names |
| `src/group.ts` | Group pods by version |
| `src/events.ts` | Detect what changed between two snapshots |
| `src/render.ts` | Draw everything into the DOM |
| `src/socket.ts` | WebSocket with auto-reconnect |
| `src/main.ts` | Connects all pieces together |
| `src/style.css` | Looks and animations |

---

## `index.html`

```html
<!-- frontend/index.html lines 1-12 -->
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Deploy Strategies Live</title>
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/src/main.ts"></script>
  </body>
</html>
```

This is the whole HTML file. It has one empty `<div id="app">`; JavaScript fills it. The `<script type="module">` loads `main.ts`. The browser cannot run `.ts` files, but Vite understands this tag: in development it converts TypeScript on the fly, and `npm run build` bundles everything into `dist/`, which the Go dashboard serves. `viewport` makes the page behave on phones.

---

## `vite.config.ts`

```ts
// frontend/vite.config.ts lines 1-4
import { defineConfig } from 'vitest/config';

// During `npm run dev`, forward /ws and /api to the dashboard running in the kind cluster.
const dashboard = process.env.DASHBOARD_URL ?? 'http://localhost:30080';
```

`defineConfig` gives type hints while editing the config. We import it from `vitest/config` (not `vite`) because the same file also configures tests. Line 4 reads an environment variable with a default: while developing on your laptop, the page runs on the Vite server, but the data comes from the dashboard in the kind cluster at port 30080.

```ts
// frontend/vite.config.ts lines 6-14
export default defineConfig({
  server: {
    proxy: {
      '/ws': { target: dashboard, ws: true },
      '/api': { target: dashboard },
    },
  },
  test: { environment: 'jsdom' },
});
```

The `proxy` forwards `/ws` (with `ws: true` so WebSocket upgrades work) and `/api` to the dashboard. The browser thinks everything comes from one server, so we avoid cross-origin problems. The `test` block says unit tests run in `jsdom`, a fake browser DOM written in JavaScript.

---

## `src/types.ts`

```ts
// frontend/src/types.ts lines 1-3
// Mirrors the JSON produced by dashboard/internal/state/snapshot.go.

export type PodStatus = 'Pending' | 'Starting' | 'Ready' | 'Terminating' | 'Failed';
```

A comment reminds us that these types must mirror the Go structs. If you change JSON on the server, change them here too. `PodStatus` is a **union of strings**: TypeScript will complain if you write a status that does not exist, for example `'Runing'`.

```ts
// frontend/src/types.ts lines 5-16
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
```

`PodView` is one pod. `Record<string, string>` is an object used as a dictionary (key text to value text). Field names are exactly the JSON keys from the Go `json:"..."` tags.

```ts
// frontend/src/types.ts lines 18-38
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
```

The traffic share per version, the traffic summary and the whole `Snapshot`. `time` is a string because JSON has no date type.

---

## `src/format.ts`

```ts
// frontend/src/format.ts lines 1-5
// "color-backend-blue-7d9f8c6b5-x2kq9" → "blue-7d9f8c6b5-x2kq9"
// The middle part is the ReplicaSet hash: it changes on every rollout, which is worth pointing out in class.
export function shortName(podName: string): string {
  return podName.replace(/^color-backend-/, '');
}
```

Pod names are long, such as `color-backend-blue-7d9f8c6b5-x2kq9`. The regex `/^color-backend-/` matches that prefix only at the start (`^`), and `replace` removes it. The remaining middle part is the ReplicaSet hash, which changes with each rollout, so students can see that a new rollout creates new pods.

---

## `src/group.ts`

### Types and sorting helper

```ts
// frontend/src/group.ts lines 1-10
import type { PodView, Snapshot } from './types';

export interface VersionGroup {
  version: string;
  color: string;
  pods: PodView[];
  trafficPercent: number;
}

const byNaturalOrder = (a: string, b: string) => a.localeCompare(b, undefined, { numeric: true });
```

A `VersionGroup` is the data for one panel: the version, its color, its pods and its share of traffic. `byNaturalOrder` compares text with `numeric: true`, so `v2` comes before `v10` (plain alphabetical order would put `v10` first).

### Building the groups

```ts
// frontend/src/group.ts lines 12-15
// One group (= one panel on screen) per version, e.g. v1 and v2 during a rollout.
export function groupByVersion(snap: Snapshot): VersionGroup[] {
  const traffic = new Map(snap.traffic.byVersion.map((v) => [v.version, v]));
  const groups = new Map<string, VersionGroup>();
```

We turn the traffic list into a `Map` for quick lookup by version. `new Map(array.map(...))` builds a dictionary from `[key, value]` pairs. `groups` will hold one entry per version.

```ts
// frontend/src/group.ts lines 16-27
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
```

For each pod we find its group or create it. A new group takes its color and traffic percent from the traffic data (`?.` and `??` protect against a version with no traffic yet, so it shows 0%). If the pod knows its own color, that one wins, because it came from the pod itself. At the end we sort the groups by version, so panels never swap places.

---

## `src/events.ts`

### Finding the traffic leader

```ts
// frontend/src/events.ts lines 1-6
import { shortName } from './format';
import type { Snapshot, VersionShare } from './types';

function leader(snap: Snapshot): VersionShare | undefined {
  return [...snap.traffic.byVersion].sort((a, b) => b.percent - a.percent)[0];
}
```

`leader` returns the version with the highest traffic percent. The spread `[...list]` makes a copy first, because `sort` changes the array in place and we must not change the snapshot. The result may be `undefined` if there is no traffic, so the return type says `VersionShare | undefined`.

### Comparing two snapshots

```ts
// frontend/src/events.ts lines 8-13
// Compare two snapshots and describe what changed, in the order a student would notice it.
export function diffEvents(prev: Snapshot | null, next: Snapshot): string[] {
  if (!prev) return [];
  const events: string[] = [];
  const before = new Map(prev.pods.map((p) => [p.name, p]));

```

With no previous snapshot (first message after connect) there is nothing to compare, so we return no events. Otherwise `before` is a map of the old pods by name.

```ts
// frontend/src/events.ts lines 14-25
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
```

Walk through the new pods. A pod not in `before` is **created**. A pod whose status differs is a **status change**, e.g. `Starting → Ready`. After checking a pod we delete it from `before`. What is left in `before` at the end are pods that are no longer in the new snapshot: **removed**. This is a classic diff pattern: one pass, no nested loops.

### Traffic events

```ts
// frontend/src/events.ts lines 27-33
  const was = leader(prev);
  const now = leader(next);
  if (now && now.percent >= 50 && was?.version !== now.version) {
    events.push(`traffic: ${now.version} now serves ${Math.round(now.percent)}%`);
  }
  return events;
}
```

We announce a traffic change only when the leader changes **and** has at least 50%. Without the 50% rule, tiny wobbles during a canary (for example 11% vs 10%) would flood the log. Doing the diff on the client keeps the server simple: it only sends the current state.

---

## `src/render.ts`

### Constants and a tiny helper

```ts
// frontend/src/render.ts lines 1-12
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
```

`FALLBACK_COLOR` is gray, used when a color is not known yet. `el(tag, className)` creates an element and sets its class. We use this helper many times, so the code stays short.

### `syncChildren`: update the DOM without flicker

```ts
// frontend/src/render.ts lines 14-26
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
```

This is the most important function in the file. A simple way to redraw would be: delete all children and create new ones every 500 ms. That works, but it **restarts every CSS animation** (the moving stripes, the fade-in) and the page flickers. `syncChildren` instead **re-uses** the elements that already exist. Each element remembers its identity in `data-key`. `<T>` makes the function generic: it works for pods, groups, or traffic segments, with functions passed in for `key`, `create` and `update`. First step: collect the existing children into a map by key.

```ts
// frontend/src/render.ts lines 27-41
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
```

For each wanted item, in order: if an element with that key exists, take it (and remove from the "unused" map). Otherwise create a new one and stamp the key. Then `update` refreshes its text and styles. Finally we make sure the node is at the right position: `parent.children[index]` is what stands there now, and if it is not our node, `insertBefore` moves it. At the very end, whatever is still in `existing` was not wanted any more, so it is removed.

**Before/after example.** A grid shows three pods A, B, C. The next snapshot has B, C and a new pod D (A was deleted).

```
DOM before:      [A] [B] [C]          wanted keys: B, C, D

index 0, key B   B exists. DOM has A at 0, not B  -> move B in front of A
                 DOM: [B] [A] [C]
index 1, key C   C exists. DOM has A at 1, not C  -> move C in front of A
                 DOM: [B] [C] [A]
index 2, key D   D is new -> create it. DOM has A at 2 -> insert D before A
                 DOM: [B] [C] [D] [A]
cleanup          A was never claimed -> remove A
DOM after:       [B] [C] [D]
```

B and C are the **same DOM elements** as before, so their animations were not interrupted. Only D is new (it plays the pop-in animation) and A is gone.

### Traffic bar

```ts
// frontend/src/render.ts lines 43-45
function shareColor(v: VersionShare): string {
  return v.version === 'unreachable' ? UNREACHABLE_COLOR : v.color || FALLBACK_COLOR;
}
```

Version `unreachable` (no pod answered) gets a dark color so downtime looks different from a real version. Others use their color, or the fallback.

```ts
// frontend/src/render.ts lines 47-55
export function renderTraffic(panel: Element, traffic: TrafficSummary): void {
  const bar = panel.querySelector('.traffic-bar')!;
  const legend = panel.querySelector('.traffic-legend')!;

  syncChildren(bar, traffic.byVersion, (v) => v.version, () => el('div', 'segment'), (seg, v) => {
    seg.style.width = `${v.percent}%`;
    seg.style.background = shareColor(v);
    seg.textContent = v.percent >= 8 ? `${v.version} ${Math.round(v.percent)}%` : '';
  });
```

We find the bar and legend by CSS selector. The `!` says "these exist for sure" (main.ts creates them). The bar uses `syncChildren` keyed by version: each segment gets a `width` in percent and a background color. Because the CSS has `transition: width`, the segment glides smoothly to its new size. We write text inside only when the segment is at least 8% wide, otherwise the label would not fit.

```ts
// frontend/src/render.ts lines 57-73
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
```

The legend is the same idea with a more complex `create`: each item has a color swatch and a text span. `classList.toggle('has-errors', condition)` adds or removes the class by a true/false value (errors turn the item red). The text looks like `v3: 70.0% (70 req, 21 errors)`. The color goes into a CSS custom property `--swatch` so the stylesheet decides how to use it.

### Pod boxes

```ts
// frontend/src/render.ts lines 75-80
export function badgeText(p: PodView): string {
  if (p.status !== 'Ready') return p.status;
  if (p.errors > 0) return `${p.errors} errors`;
  if (p.hits === 0) return 'no traffic';
  return 'serving';
}
```

`badgeText` chooses the small label in the box. Not Ready: show the state name. Ready with errors: show the number of errors. Ready with zero hits: `no traffic` (the idle side of a blue/green). Otherwise `serving`. The order of the checks is the order of importance.

```ts
// frontend/src/render.ts lines 82-86
function createPod(): HTMLElement {
  const box = el('div', 'pod');
  box.append(el('span', 'pod-badge'), el('span', 'pod-name'), el('span', 'pod-meta'));
  return box;
}
```

`createPod` builds the empty box once: a badge, a name and a meta line. `append` can take several children at once.

```ts
// frontend/src/render.ts lines 88-100
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
```

`updatePod` runs every 500 ms for every pod, so it only sets values. `dataset.status` becomes the HTML attribute `data-status`, which CSS uses to choose the look (see the CSS section). `--pod-color` is a CSS variable holding this pod's color. Two classes are toggled: `idle` and `erroring`. The `title` is the hover tooltip with details. Of all labels, only `slot` (blue/green) and `track` (canary) are shown in the box, since those are the ones that teach something.

### Groups, events

```ts
// frontend/src/render.ts lines 102-114
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
```

A group (panel) has a title and a grid. `updateGroup` sets the panel color, counts Ready pods, writes a title like `v2 · 3/4 ready · 40% of traffic`, and calls `syncChildren` again for the pods inside. So we have **nested keyed updates**: groups keyed by version, pods keyed by name.

```ts
// frontend/src/render.ts lines 116-126
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
```

`renderGroups` is the top-level call. `renderEvents` is simpler: the event list is short (max 40 lines) and has no animations, so we just replace all `<li>` items. `replaceChildren(...)` swaps all children in one step, and `textContent` (not `innerHTML`) means text can never be interpreted as HTML.

---

## `src/socket.ts`

```ts
// frontend/src/socket.ts lines 1-6
import type { Snapshot } from './types';

// Build the WebSocket URL from the page URL, so it works on localhost, a NodePort, or behind an ALB.
export function wsUrl(loc: Pick<Location, 'protocol' | 'host'>): string {
  return `${loc.protocol === 'https:' ? 'wss' : 'ws'}://${loc.host}/ws`;
}
```

`wsUrl` chooses `wss://` (secure) if the page is `https:`, otherwise `ws://`, and uses the same host as the page. So the same code works on localhost, on the NodePort and behind a load balancer. The parameter type `Pick<Location, 'protocol' | 'host'>` asks only for the two fields we need, which makes the function easy to test with a small fake object.

```ts
// frontend/src/socket.ts lines 8-17
// Connect and keep reconnecting (0.5 s, 1 s, 2 s … max 5 s) if the dashboard restarts.
export function connect(
  url: string,
  onSnapshot: (snap: Snapshot) => void,
  onStatus: (connected: boolean) => void,
): () => void {
  let socket: WebSocket | null = null;
  let stopped = false;
  let delay = 500;

```

`connect` takes the URL and two callbacks, and returns a function that stops everything. `delay` is the wait before the next reconnect attempt.

```ts
// frontend/src/socket.ts lines 18-31
  const open = () => {
    socket = new WebSocket(url);
    socket.onopen = () => {
      delay = 500;
      onStatus(true);
    };
    socket.onmessage = (event) => onSnapshot(JSON.parse(event.data as string) as Snapshot);
    socket.onclose = () => {
      onStatus(false);
      if (stopped) return;
      setTimeout(open, delay);
      delay = Math.min(delay * 2, 5000);
    };
  };
```

`new WebSocket(url)` opens the connection. When it opens we reset the delay and tell the page we are connected. Each message is JSON text; we parse it and pass it on. When the connection closes (for example the dashboard restarts during a lesson), we show "reconnecting", wait, and try again. The delay doubles each time but never goes above 5 seconds. This is called **exponential backoff**: it avoids hammering a server that is down, but still comes back quickly.

```ts
// frontend/src/socket.ts lines 33-38
  open();
  return () => {
    stopped = true;
    socket?.close();
  };
}
```

We try to connect right away and return a `stop` function. It sets `stopped` first so the `onclose` handler does not start a new reconnect.

---

## `src/main.ts`

### Imports and the page skeleton

```ts
// frontend/src/main.ts lines 1-8
import './style.css';
import { diffEvents } from './events';
import { groupByVersion } from './group';
import { renderEvents, renderGroups, renderTraffic } from './render';
import { connect, wsUrl } from './socket';
import type { Snapshot } from './types';

const app = document.querySelector<HTMLDivElement>('#app')!;
```

The first line imports the CSS: Vite bundles it. `import type` imports a type only, and it disappears at build time. `querySelector<HTMLDivElement>('#app')!` finds the empty div from `index.html`.

```ts
// frontend/src/main.ts lines 9-32
app.innerHTML = `
  <header>
    <h1>Deployment Strategies · Live</h1>
    <span id="conn" class="conn">connecting…</span>
    <span id="meta" class="meta"></span>
  </header>
  <section id="traffic" class="panel traffic">
    <h2>Traffic through the Service <code id="target"></code></h2>
    <div class="traffic-bar"></div>
    <div class="traffic-legend"></div>
  </section>
  <main id="groups"></main>
  <aside class="panel events">
    <h2>Events</h2>
    <ol id="events"></ol>
  </aside>
  <footer class="legend">
    <span><i class="pod mini" data-status="Pending"></i> Pending</span>
    <span><i class="pod mini" data-status="Starting"></i> Starting (not ready)</span>
    <span><i class="pod mini" data-status="Ready"></i> Ready &amp; serving</span>
    <span><i class="pod mini idle" data-status="Ready"></i> Ready, no traffic</span>
    <span><i class="pod mini" data-status="Terminating"></i> Terminating</span>
    <span><i class="pod mini erroring" data-status="Ready"></i> Returning errors</span>
  </footer>`;
```

We create the permanent page structure once with a template string (backticks allow multiple lines). It has a header with the connection badge, the traffic panel, an empty `<main id="groups">`, the events list and a legend footer. The legend reuses the same `.pod` classes and `data-status` values as real boxes, so the legend always looks like the real thing. Later updates only touch the parts inside these elements.

### State and helper

```ts
// frontend/src/main.ts lines 34-38
const MAX_EVENTS = 40;
let previous: Snapshot | null = null;
let eventLog: string[] = [];

const $ = <T extends Element>(selector: string) => document.querySelector<T>(selector)!;
```

Two pieces of state live outside any function: the previous snapshot (needed for the diff) and the event log. `$` is a small helper for `querySelector` that keeps the lines below short; the `<T extends Element>` lets the caller say what kind of element to expect.

### The snapshot handler

```ts
// frontend/src/main.ts lines 40-47
connect(
  wsUrl(window.location),
  (snap) => {
    const stamp = new Date().toLocaleTimeString();
    const fresh = diffEvents(previous, snap).map((e) => `${stamp}  ${e}`);
    eventLog = [...fresh.reverse(), ...eventLog].slice(0, MAX_EVENTS); // newest first
    previous = snap;

```

For each message: add a timestamp to each new event, put the new ones first (`reverse()` then the old log), and keep at most 40 lines using `slice`. Then remember this snapshot for the next diff.

```ts
// frontend/src/main.ts lines 48-60
    renderTraffic($('#traffic'), snap.traffic);
    renderGroups($('#groups'), groupByVersion(snap));
    renderEvents($('#events'), eventLog);
    $('#target').textContent = snap.target;
    $('#meta').textContent =
      `namespace ${snap.namespace} · ${snap.pods.length} pods · last ${snap.traffic.total} requests`;
  },
  (connected) => {
    const badge = $('#conn');
    badge.textContent = connected ? 'live' : 'reconnecting…';
    badge.classList.toggle('ok', connected);
  },
);
```

Then redraw: traffic bar, version groups, event list, target URL and the meta text. The second callback only updates the connection badge. Notice how little code is here: the real work is in the small modules. Data flows in one direction: **snapshot in, DOM out**.

---

## `src/style.css`

### Variables, layout and header

```css
/* frontend/src/style.css lines 1-14 */
:root {
  --bg: #f8fafc;
  --panel: #ffffff;
  --text: #0f172a;
  --muted: #64748b;
  --border: #e2e8f0;
  --danger: #dc2626;
  --fallback: #9ca3af;
  font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  color: var(--text);
  background: var(--bg);
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); }
```

CSS **custom properties** (`--bg`) are variables defined on `:root` and used with `var(--bg)`. Changing a color in one place changes the whole page. `box-sizing: border-box` makes width include padding, which makes sizes easier to predict.

```css
/* frontend/src/style.css lines 16-29 */
#app {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
  grid-template-areas: "header header" "traffic traffic" "groups events" "legend legend";
  gap: 16px;
  padding: 16px;
  max-width: 1500px;
  margin: 0 auto;
}
header { grid-area: header; display: flex; align-items: baseline; gap: 16px; flex-wrap: wrap; }
header h1 { margin: 0; font-size: 1.6rem; }
.conn { padding: 2px 10px; border-radius: 999px; background: #fee2e2; color: var(--danger); font-size: .85rem; }
.conn.ok { background: #dcfce7; color: #166534; }
.meta { color: var(--muted); font-size: .9rem; }
```

`#app` is a CSS **grid** with named areas: header on top, traffic below, then groups on the left with the events column (340px) on the right, and the legend at the bottom. `minmax(0, 1fr)` lets the left column shrink instead of overflowing. The connection badge is red by default and green with `.ok`.

### Panels and the traffic bar

```css
/* frontend/src/style.css lines 31-43 */
.panel { background: var(--panel); border: 1px solid var(--border); border-radius: 12px; padding: 16px; }
.panel h2 { margin: 0 0 12px; font-size: 1.05rem; }

.traffic { grid-area: traffic; }
.traffic-bar { display: flex; height: 48px; border-radius: 8px; overflow: hidden; background: var(--border); }
.segment {
  display: grid; place-items: center; color: #fff; font-weight: 700; white-space: nowrap; overflow: hidden;
  text-shadow: 0 1px 2px rgb(0 0 0 / .5); transition: width .4s ease;
}
.traffic-legend { display: flex; gap: 18px; flex-wrap: wrap; margin-top: 10px; font-size: .95rem; }
.legend-item { display: flex; align-items: center; gap: 6px; }
.swatch { width: 14px; height: 14px; border-radius: 4px; background: var(--swatch); }
.legend-item.has-errors { color: var(--danger); font-weight: 600; }
```

`.panel` is the white card used everywhere. The traffic bar is a flex row; each `.segment` gets its width from JavaScript, and `transition: width .4s` animates changes. `overflow: hidden` and `border-radius` clip the segments to rounded ends. The legend swatch takes its color from the `--swatch` variable set in `render.ts`.

```css
/* frontend/src/style.css lines 45-47 */
#groups { grid-area: groups; display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.group { border-left: 8px solid var(--group-color, var(--fallback)); }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 12px; }
```

Each version panel has a thick left border in the version color (`--group-color`, set by `updateGroup`). The pod grid uses `repeat(auto-fill, minmax(160px, 1fr))`: as many columns of at least 160px as fit, so it adapts to the window size.

### The pod box and its states

```css
/* frontend/src/style.css lines 49-58 */
.pod {
  position: relative; min-height: 100px; border-radius: 10px; padding: 10px;
  display: flex; flex-direction: column; justify-content: space-between; gap: 6px;
  color: #fff; background: var(--pod-color, var(--fallback)); text-shadow: 0 1px 2px rgb(0 0 0 / .45);
  transition: opacity .3s, transform .3s, box-shadow .3s;
  animation: pop-in .3s ease-out;
}
.pod-name { font-weight: 700; font-size: .85rem; word-break: break-all; }
.pod-meta { font-size: .75rem; opacity: .95; }
.pod-badge { align-self: flex-start; font-size: .72rem; padding: 2px 8px; border-radius: 999px; background: rgb(0 0 0 / .25); }
```

The base `.pod` is a solid colored card: the background is `var(--pod-color, var(--fallback))` (the second value is used when the variable is missing). White text with a soft shadow stays readable on every color. `transition` smooths opacity, size and shadow changes, and `pop-in` plays once when a new box appears. This is why `syncChildren` matters: only new elements get this animation.

```css
/* frontend/src/style.css lines 60-63 */
.pod[data-status="Pending"] {
  background: transparent; color: var(--text); text-shadow: none;
  border: 3px dashed var(--pod-color, var(--fallback));
}
```

**Pending**: the pod was accepted but has no node, image or running container yet. The box is **transparent with a dashed outline** in the version color, like an empty "reserved seat". The text color is switched to dark because the background no longer helps readability.

```css
/* frontend/src/style.css lines 64-68 */
.pod[data-status="Starting"] {
  background: repeating-linear-gradient(45deg, var(--pod-color, var(--fallback)) 0 12px, rgb(255 255 255 / .55) 12px 24px);
  background-size: 34px 34px;
  animation: stripes 1s linear infinite;
}
```

**Starting**: the container is running but the readiness probe is not passing, so it receives no traffic yet. The box shows **animated diagonal stripes** (a repeating gradient mixed with white) that move. The `stripes` animation shifts the background by one pattern width (34px) each second in a loop, which looks like "work in progress".

```css
/* frontend/src/style.css line 69 */
.pod[data-status="Terminating"] { opacity: .35; transform: scale(.92); filter: grayscale(.7); }
```

**Ready** has no special rule: it is the default, a **solid color box**. That is on purpose: the normal healthy state is the plain one, and every other state is a variation. Ready boxes are the only ones counted in the panel title.

**Terminating**: the line above makes the box **faded to 35%, slightly smaller and gray**. It looks like it is disappearing, which matches what happens: Kubernetes is shutting it down (and the `preStop` sleep keeps it alive for a few seconds).

```css
/* frontend/src/style.css line 70 */
.pod[data-status="Failed"] { outline: 4px solid var(--danger); }
```

**Failed**: a thick **red outline** (`outline`, which does not change the box size) in the danger color. The container exited with an error and nobody is serving from it.

```css
/* frontend/src/style.css lines 71-73 */
.pod.idle { opacity: .55; }
.pod.idle .pod-badge { background: rgb(255 255 255 / .9); color: var(--text); text-shadow: none; }
.pod.erroring { box-shadow: 0 0 0 4px var(--danger); }
```

Two modifiers on Ready boxes. **Idle** (`no traffic`): the box is **dimmed to 55%** and its badge turns white. It is ready but receives 0 requests, as happens with the unused slot in blue/green or a canary with a tiny share. **Erroring**: a **red ring** (a `box-shadow` with 0 blur, like a border) around the box; it is added when the pod returned errors, which is how v3 is easy to spot.

### Events, legend and animations

```css
/* frontend/src/style.css lines 75-82 */
.events { grid-area: events; max-height: 75vh; overflow: auto; }
#events { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .8rem; }

.legend { grid-area: legend; display: flex; gap: 18px; flex-wrap: wrap; color: var(--muted); font-size: .85rem; }
.legend span { display: flex; align-items: center; gap: 6px; }
.pod.mini { --pod-color: #64748b; min-height: 0; width: 22px; height: 16px; padding: 0; border-radius: 4px; animation: none; }
.pod.mini[data-status="Starting"] { animation: stripes 1s linear infinite; }
```

The events panel scrolls inside a maximum height (75% of the window height) and uses a monospace font so the arrows and times line up. The footer legend reuses `.pod` as a tiny 22x16 chip (`.mini`), with animations off, except Starting, whose stripes should still move.

```css
/* frontend/src/style.css lines 84-89 */
@keyframes stripes { to { background-position: 34px 0; } }
@keyframes pop-in { from { transform: scale(.6); opacity: 0; } }

@media (max-width: 900px) {
  #app { grid-template-columns: 1fr; grid-template-areas: "header" "traffic" "groups" "events" "legend"; }
}
```

`@keyframes` define the animations. `stripes` moves the background to the right by one tile; `pop-in` starts small and invisible (`from` only, the end is the normal state). The media query changes the grid to a single column on screens narrower than 900px, so the page works on a phone.

---

## Try it yourself

Run these in `frontend/`. For exercises 1-3 you need `npm install` done once. Exercise 4 needs the cluster from `make up`.

1. **Run the unit tests.** `npm test` (or `npx vitest run`). Expected: all test files pass, including `group`, `events`, `render` and `socket`.
2. **Test `wsUrl` in the browser console.** Open the dashboard, press F12, and compare `location.protocol` and `location.host` with the URL the WebSocket uses in the Network tab. Expected: on `http://localhost:30080` the URL is `ws://localhost:30080/ws`.
3. **Change a color.** In `style.css`, change the Terminating rule's `opacity: .35` to `opacity: 1`. Run `npm run dev` while a rolling update is running. Expected: terminating pods are no longer faded (but still smaller and gray). Change it back.
4. **Watch keys at work.** With the dashboard open, run `make rolling-start` and then `make upgrade TO=v2`. In the browser developer tools (Elements tab), select one `.pod` and keep watching it. Expected: while other boxes appear and disappear, the element you selected stays the same node with the same `data-key`; only its attributes change.
5. **Break the reconnect.** `kubectl -n color-demo rollout restart deploy/color-dashboard`. Expected: the badge turns red `reconnecting…` for a few seconds, then green `live`, without reloading the page.
