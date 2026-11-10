import type { ReactNode } from 'react';
import { filesText as T } from './texts';

export type MessageType = 'success' | 'error';
export type ShowMessage = (text: string, type: MessageType) => void;

const messageColors: Record<MessageType, string> = {
  success: 'border-green-300 bg-green-100 text-green-900 dark:border-green-800 dark:bg-green-950 dark:text-green-200',
  error: 'border-red-300 bg-red-100 text-red-900 dark:border-red-800 dark:bg-red-950 dark:text-red-200',
};

export function Message({ type, children }: { type: MessageType; children: ReactNode }) {
  return <div className={`message ${type} my-2 rounded border p-3 ${messageColors[type]}`}>{children}</div>;
}

export const buttonBase = 'cursor-pointer rounded px-4 py-2 text-white disabled:cursor-not-allowed disabled:opacity-50 ';
export const primaryButton = buttonBase + 'bg-blue-600 hover:bg-blue-700';
export const grayButton = buttonBase + 'bg-gray-500 hover:bg-gray-600';
export const inputClass = 'rounded border border-[var(--border)] bg-[var(--card)] px-2 py-1.5 text-[var(--fg)]';

// A pop-up with a title and a close button. Clicking the dark area closes it.
export function ModalShell(props: { id: string; title: string; titleId: string; wide?: boolean; onClose: () => void; children: ReactNode }) {
  return (
    <div
      id={props.id}
      className="fixed left-0 top-0 z-40 flex h-full w-full items-start justify-center overflow-auto bg-black/50 p-3 md:p-8"
      onClick={props.onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        className={
          'w-full rounded-lg bg-[var(--card)] p-4 shadow-lg ' + (props.wide ? 'max-w-[1200px]' : 'max-w-4xl')
        }
        onClick={(event) => event.stopPropagation()}
      >
        <div className="mb-3 flex items-start justify-between gap-3 pr-0">
          <h2 id={props.titleId} className="m-0 min-w-0 break-words text-lg font-bold">
            {props.title}
          </h2>
          <button type="button" className="close cursor-pointer border-0 bg-transparent text-3xl leading-none" aria-label={T.close} onClick={props.onClose}>
            &times;
          </button>
        </div>
        {props.children}
      </div>
    </div>
  );
}
