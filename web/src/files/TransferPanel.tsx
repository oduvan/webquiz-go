import { useEffect, useRef, useState } from 'react';
import { getWithSession, readJson, sendJson, type TransferState } from './api';
import { filesText as T } from './texts';
import { inputClass, primaryButton, type ShowMessage } from './ui';

const TARGET_KEY = 'webquiz-transfer-target';
const POLL_MS = 5000;

// The panel stays hidden until the server says transfer is available (the tunnel is connected).
export function TransferPanel({
  selected,
  onSent,
  onAvailable,
  showMessage,
}: {
  selected: Set<string>;
  onSent: () => void;
  onAvailable: (available: boolean) => void;
  showMessage: ShowMessage;
}) {
  const [state, setState] = useState<TransferState | null>(null);
  const [target, setTarget] = useState(() => {
    try {
      return localStorage.getItem(TARGET_KEY) || '';
    } catch (e) {
      return '';
    }
  });
  const [sending, setSending] = useState(false);
  const statuses = useRef<Record<string, string>>({});
  const messageRef = useRef(showMessage);
  messageRef.current = showMessage;
  const availableRef = useRef(onAvailable);
  availableRef.current = onAvailable;

  const load = async () => {
    try {
      const response = await getWithSession('/api/admin/transfer');
      if (!response.ok) return;
      const next = await readJson<TransferState>(response);
      // Tell the admin when the other server answers
      next.outgoing.forEach((offer) => {
        const previous = statuses.current[offer.id];
        if (previous === 'waiting' && offer.status !== 'waiting') {
          messageRef.current(`${offer.to}: ${T.transferStatus[offer.status] || offer.status}`, offer.status === 'accepted' ? 'success' : 'error');
        }
        statuses.current[offer.id] = offer.status;
      });
      setState(next);
      availableRef.current(!!next.available);
    } catch (e) {
      // Ignore: the next poll will try again
    }
  };

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), POLL_MS);
    return () => window.clearInterval(timer);
  }, []);

  const send = async () => {
    const name = target.trim();
    if (selected.size === 0) {
      showMessage(T.transferSelectFirst, 'error');
      return;
    }
    if (!name) {
      showMessage(T.transferEnterName, 'error');
      return;
    }
    const files = [...selected].map((key) => {
      const slash = key.indexOf('/');
      return { type: key.slice(0, slash), name: key.slice(slash + 1) };
    });
    setSending(true);
    try {
      const response = await sendJson('/api/admin/transfer/send', 'POST', { to: name, files });
      const data = await readJson<{ offer?: { files: { type: string }[] }; error?: string }>(response);
      if (response.ok) {
        const media = (data.offer?.files ?? []).filter((file) => file.type === 'images' || file.type === 'attachments').length;
        showMessage(T.transferSent(name, media), 'success');
        try {
          localStorage.setItem(TARGET_KEY, name);
        } catch (e) {
          // Storage may be blocked
        }
        onSent();
        await load();
      } else {
        showMessage(data.error || T.transferFailed, 'error');
      }
    } catch (error) {
      showMessage(T.connectionError + (error as Error).message, 'error');
    } finally {
      setSending(false);
    }
  };

  if (!state || !state.available) return null;

  return (
    <div id="transfer-panel" className="transfer-panel mb-4 rounded border border-[var(--border)] p-3">
      <strong>{T.transferTitle}</strong>
      <div className="mt-1 text-sm opacity-80">
        {T.transferNote} {T.transferThisServer} <strong id="transfer-own-name">{state.name ?? ''}</strong>.
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <span id="transfer-selected-count">{T.transferSelected(selected.size)}</span>
        <input
          type="text"
          id="transfer-target"
          value={target}
          placeholder={T.transferTargetPlaceholder}
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          onChange={(event) => setTarget(event.target.value)}
          className={inputClass + ' min-w-0 flex-1'}
        />
        <button type="button" id="transfer-send-btn" className={primaryButton} disabled={sending} onClick={() => void send()}>
          {T.transferSend}
        </button>
      </div>
      <div id="transfer-outgoing" className="mt-2 text-sm">
        {state.outgoing.slice(0, 5).map((offer) => (
          <div key={offer.id} className="transfer-outgoing-item">
            → <strong>{offer.to}</strong>: {T.transferFiles(offer.files.length)}, {T.transferStatus[offer.status] || offer.status}
          </div>
        ))}
      </div>
    </div>
  );
}
