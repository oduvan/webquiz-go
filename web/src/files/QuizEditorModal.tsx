import { useEffect, useRef, useState } from 'react';
import { getWithSession, readJson, sendJson } from './api';
import { filesText as T } from './texts';
import { grayButton, inputClass, Message, ModalShell, primaryButton, type ShowMessage } from './ui';

type Notice = { type: 'success' | 'error'; text: string; errors?: string[] } | null;

export function QuizEditorModal({
  filename,
  onClose,
  onSaved,
  showMessage,
}: {
  filename: string;
  onClose: () => void;
  onSaved: () => Promise<void> | void;
  showMessage: ShowMessage;
}) {
  const [content, setContent] = useState('');
  const [notice, setNotice] = useState<Notice>(null);
  const closeTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const response = await getWithSession(`/api/files/quizzes/view/${encodeURIComponent(filename)}`);
        if (cancelled) return;
        if (response.ok) {
          setContent(await response.text());
        } else {
          const data = await readJson<{ error?: string }>(response);
          setNotice({ type: 'error', text: T.errorPrefix + (data.error ?? '') });
        }
      } catch (error) {
        if (!cancelled) setNotice({ type: 'error', text: T.errorLoadingFile + (error as Error).message });
      }
    })();
    return () => {
      cancelled = true;
      window.clearTimeout(closeTimer.current);
    };
  }, [filename]);

  const validate = async () => {
    if (!content.trim()) {
      setNotice({ type: 'error', text: T.contentEmpty });
      return;
    }
    try {
      const response = await sendJson('/api/admin/validate-quiz', 'POST', { content: content.trim() });
      const data = await readJson<{ valid?: boolean; question_count?: number; errors?: string[]; error?: string }>(response);
      if (response.ok) {
        if (data.valid) {
          setNotice({ type: 'success', text: T.validYaml(data.question_count ?? 0) });
        } else {
          setNotice({ type: 'error', text: T.validationErrors, errors: data.errors ?? [] });
        }
      } else {
        setNotice({ type: 'error', text: T.errorPrefix + (data.error ?? '') });
      }
    } catch (error) {
      setNotice({ type: 'error', text: T.errorPrefix + (error as Error).message });
    }
  };

  const save = async () => {
    if (!content.trim()) {
      setNotice({ type: 'error', text: T.contentCannotBeEmpty });
      return;
    }
    try {
      const response = await sendJson(`/api/files/quizzes/save/${encodeURIComponent(filename)}`, 'PUT', { content });
      const data = await readJson<{ message?: string; error?: string; errors?: string[] }>(response);
      if (response.ok) {
        setNotice({ type: 'success', text: data.message ?? '' });
        showMessage(T.saved(filename), 'success');
        await onSaved();
        closeTimer.current = window.setTimeout(onClose, 2000);
      } else {
        setNotice({ type: 'error', text: data.error || T.failedToSaveQuiz, errors: data.errors });
      }
    } catch (error) {
      setNotice({ type: 'error', text: T.errorPrefix + (error as Error).message });
    }
  };

  return (
    <ModalShell id="quiz-editor-modal" titleId="quiz-editor-title" title={T.editQuiz(filename)} onClose={onClose}>
      <div className="warning message my-2 rounded border border-yellow-400 bg-yellow-100 p-3 text-yellow-900">{T.editNote}</div>
      <div className="my-3">
        <label htmlFor="quiz-editor-content" className="mb-1 block font-bold">
          {T.quizYaml}
        </label>
        <textarea
          id="quiz-editor-content"
          rows={20}
          value={content}
          onChange={(event) => setContent(event.target.value)}
          className={inputClass + ' box-border w-full font-mono text-sm'}
        />
      </div>
      <div className="flex flex-wrap gap-2">
        <button type="button" className={primaryButton} onClick={() => void validate()}>
          {T.validate}
        </button>
        <button type="button" className={primaryButton} onClick={() => void save()}>
          {T.save}
        </button>
        <button type="button" className={grayButton} onClick={onClose}>
          {T.cancel}
        </button>
      </div>
      <div id="quiz-editor-message-area">
        {notice && (
          <Message type={notice.type}>
            {notice.text}
            {notice.errors && notice.errors.length > 0 && (
              <ul className="ml-5 list-disc">
                {notice.errors.map((error, index) => (
                  <li key={index}>{error}</li>
                ))}
              </ul>
            )}
          </Message>
        )}
      </div>
    </ModalShell>
  );
}
