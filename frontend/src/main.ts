import './style.css';
import { diffEvents } from './events';
import { groupByVersion } from './group';
import { renderEvents, renderGroups, renderTraffic } from './render';
import { connect, wsUrl } from './socket';
import type { Snapshot } from './types';

const app = document.querySelector<HTMLDivElement>('#app')!;
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

const MAX_EVENTS = 40;
let previous: Snapshot | null = null;
let eventLog: string[] = [];

const $ = <T extends Element>(selector: string) => document.querySelector<T>(selector)!;

connect(
  wsUrl(window.location),
  (snap) => {
    const stamp = new Date().toLocaleTimeString();
    const fresh = diffEvents(previous, snap).map((e) => `${stamp}  ${e}`);
    eventLog = [...fresh.reverse(), ...eventLog].slice(0, MAX_EVENTS); // newest first
    previous = snap;

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
