import { mountPage } from '../shared/mount';
import { LiveStatsClient } from './client';
import { LiveStatsPage } from './LiveStatsPage';
import { TEXTS } from './texts';
import './livestats.css';

// The live stats page is served by the server itself (local network only), never
// under the tunnel's path prefix, so the socket address starts at the host.
export function startLiveStatsPage() {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const client = new LiveStatsClient(protocol + '//' + window.location.host + '/ws/live-stats');
  client.start(); // once, before React renders (StrictMode may render twice)
  mountPage(<LiveStatsPage client={client} />, { themeTitle: TEXTS.toggleTheme });
}
