import type { Snapshot } from './types';

// Build the WebSocket URL from the page URL, so it works on localhost, a NodePort, or behind an ALB.
export function wsUrl(loc: Pick<Location, 'protocol' | 'host'>): string {
  return `${loc.protocol === 'https:' ? 'wss' : 'ws'}://${loc.host}/ws`;
}

// Connect and keep reconnecting (0.5 s, 1 s, 2 s … max 5 s) if the dashboard restarts.
export function connect(
  url: string,
  onSnapshot: (snap: Snapshot) => void,
  onStatus: (connected: boolean) => void,
): () => void {
  let socket: WebSocket | null = null;
  let stopped = false;
  let delay = 500;

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

  open();
  return () => {
    stopped = true;
    socket?.close();
  };
}
