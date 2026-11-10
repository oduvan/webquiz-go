import { useEffect, useState } from 'react';
import { csvTableUrl, getWithSession, readJson, type FileType } from './api';
import { filesText as T } from './texts';
import { ModalShell } from './ui';

export interface ViewTarget {
  type: FileType;
  name: string;
  mode: 'text' | 'table';
}

export function FileViewerModal({ target, onClose }: { target: ViewTarget; onClose: () => void }) {
  const [content, setContent] = useState<{ text: string; error: boolean } | null>(null);
  const table = target.mode === 'table' && target.type === 'csv';

  useEffect(() => {
    if (table) return;
    let cancelled = false;
    setContent(null);
    (async () => {
      try {
        const response = await getWithSession(`/api/files/${target.type}/view/${encodeURIComponent(target.name)}`);
        if (cancelled) return;
        if (response.ok) {
          setContent({ text: await response.text(), error: false });
        } else {
          const data = await readJson<{ error?: string }>(response);
          setContent({ text: T.errorPrefix + (data.error ?? ''), error: true });
        }
      } catch (error) {
        if (!cancelled) setContent({ text: T.errorLoadingFile + (error as Error).message, error: true });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [table, target.type, target.name]);

  return (
    <ModalShell id="file-viewer-modal" titleId="file-viewer-title" title={target.name} wide={table} onClose={onClose}>
      {table ? (
        // The table, its filters and the "open in new window" button live on the viewer
        // page; the pop-up shows that page in an iframe.
        <div id="file-content-table" className="csv-frame-container">
          <iframe
            id="csv-table-frame"
            title={target.name}
            src={`${csvTableUrl(target.name)}?embedded=1`}
            className="h-[70vh] w-full rounded border border-[var(--border)]"
          />
        </div>
      ) : (
        <div id="file-content" className="file-content max-h-[70vh] overflow-auto whitespace-pre-wrap break-words rounded border border-[var(--border)] p-3 font-mono text-sm">
          {content === null ? (
            <div className="loading">{T.loadingContent}</div>
          ) : content.error ? (
            <div className="error text-red-600">{content.text}</div>
          ) : (
            content.text
          )}
        </div>
      )}
    </ModalShell>
  );
}
