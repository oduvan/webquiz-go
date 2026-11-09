import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { useEffect, useState } from 'react';
import { onConnectionWaiting } from './api';

export function Button({ className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={
        'cursor-pointer rounded bg-blue-600 px-4 py-2 text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50 ' +
        className
      }
      {...props}
    />
  );
}

export function Banner({ children, tone = 'warning' }: { children: ReactNode; tone?: 'warning' | 'error' | 'info' }) {
  const colors = {
    warning: 'bg-yellow-100 text-yellow-900 border-yellow-400',
    error: 'bg-red-100 text-red-900 border-red-400',
    info: 'bg-blue-100 text-blue-900 border-blue-400',
  };
  return (
    <div role="status" className={'w-full border-b px-4 py-2 text-center ' + colors[tone]}>
      {children}
    </div>
  );
}

// Shown while any request waits for the connection (see fetchWithRetry).
export function ConnectionBanner({ text }: { text: string }) {
  const [waiting, setWaiting] = useState(false);
  useEffect(() => onConnectionWaiting(setWaiting), []);
  if (!waiting) return null;
  return (
    <div id="connection-banner">
      <Banner>{text}</Banner>
    </div>
  );
}

export function Modal({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title?: string;
  children: ReactNode;
}) {
  if (!open) return null;
  return (
    <div
      className="fixed left-0 top-0 z-40 flex h-full w-full items-center justify-center bg-black/50 p-4"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        className="max-h-full w-full max-w-lg overflow-auto rounded-lg p-4 shadow-lg"
        style={{ background: 'var(--card)' }}
        onClick={(event) => event.stopPropagation()}
      >
        {title ? <h2 className="mb-2 text-lg font-bold">{title}</h2> : null}
        {children}
      </div>
    </div>
  );
}
