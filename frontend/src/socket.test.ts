import { describe, expect, it } from 'vitest';
import { wsUrl } from './socket';

describe('wsUrl', () => {
  it('uses ws:// for http pages and wss:// for https pages', () => {
    expect(wsUrl({ protocol: 'http:', host: 'localhost:30080' })).toBe('ws://localhost:30080/ws');
    expect(wsUrl({ protocol: 'https:', host: 'demo.example.com' })).toBe('wss://demo.example.com/ws');
  });
});
