const BOOT_MARKER = '__WEBQUIZ_BOOT__';

// Reads the JSON that the Go server puts into <script id="webquiz-boot">.
// In the Vite dev server the script still holds the raw marker, so the page's
// own dev fallback is returned instead.
export function readBoot<T>(devFallback: T): T {
  const el = document.getElementById('webquiz-boot');
  const text = el && el.textContent ? el.textContent.trim() : '';
  if (text === '' || text.indexOf(BOOT_MARKER) !== -1) {
    return devFallback;
  }
  return JSON.parse(text) as T;
}
