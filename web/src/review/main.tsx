import { readBoot, type ReviewBoot } from '../shared/boot';
import { mountPage } from '../shared/mount';
import { ReviewClient } from './client';
import { ReviewPage } from './ReviewPage';
import './review.css';

// The boot data in the Vite dev server, where the Go server did not fill it in.
const DEV_BOOT: ReviewBoot = { is_trusted_ip: false, language: 'en', translations: {}, version: 'dev' };

// The review page is served by the server itself (local network only), never
// under the tunnel's path prefix, so the socket address starts at the host.
export function startReviewPage() {
  const boot = readBoot<ReviewBoot>(DEV_BOOT);
  document.documentElement.lang = boot.language;
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const client = new ReviewClient(boot.translations, boot.is_trusted_ip, protocol + '//' + window.location.host + '/ws/review');
  void client.start(); // once, before React renders (StrictMode may render twice)
  mountPage(<ReviewPage client={client} />, { themeTitle: 'Theme' });
}
