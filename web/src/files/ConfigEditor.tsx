import { useRef, useState } from 'react';
import type { ConfigFormData } from '../shared/boot';
import { readJson, sendJson } from './api';
import { filesText as T } from './texts';
import { inputClass, Message, primaryButton } from './ui';

interface FieldRow {
  key: number;
  name: string;
  enabled: boolean;
}

interface FormState {
  language: 'uk' | 'en' | 'ru';
  extraCsv: boolean;
  approve: boolean;
  usernameLabel: string;
  fields: FieldRow[];
}

type Notice = { type: 'success' | 'error'; text: string; errors?: string[] } | null;

function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.map((item) => String(item)) : [];
}

function formFromData(data: ConfigFormData | null | undefined, nextKey: () => number): FormState {
  const registration = (data?.registration ?? {}) as Record<string, unknown>;
  const language = data && data.language ? data.language : 'uk';
  const fields: FieldRow[] = [
    ...stringList(registration.fields).map((name) => ({ key: nextKey(), name, enabled: true })),
    ...stringList(registration.disabled_fields).map((name) => ({ key: nextKey(), name, enabled: false })),
  ];
  return {
    language: language === 'en' ? 'en' : 'uk',
    extraCsv: !!(data && data.extra_answers_with_users_csv),
    approve: !!registration.approve,
    usernameLabel: registration.username_label ? String(registration.username_label) : '',
    fields,
  };
}

// The body of the partial save: fields and disabled_fields come from the unified list.
export function formBody(form: FormState) {
  const fields: string[] = [];
  const disabled: string[] = [];
  form.fields.forEach((row) => {
    const name = row.name.trim();
    if (!name) return;
    (row.enabled ? fields : disabled).push(name);
  });
  return {
    language: form.language,
    extra_answers_with_users_csv: form.extraCsv,
    registration: {
      approve: form.approve,
      username_label: form.usernameLabel,
      fields,
      disabled_fields: disabled,
    },
  };
}

export function ConfigEditor(props: { path: string; content: string | null; data: ConfigFormData | null }) {
  const keyCounter = useRef(0);
  const nextKey = () => ++keyCounter.current;

  const [view, setView] = useState<'form' | 'yaml'>('form');
  const [form, setForm] = useState<FormState>(() => formFromData(props.data, nextKey));
  const [yaml, setYaml] = useState(props.content ?? '');
  const [formDirty, setFormDirty] = useState(false);
  const [yamlDirty, setYamlDirty] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);

  const change = (patch: Partial<FormState>) => {
    setForm((current) => ({ ...current, ...patch }));
    setFormDirty(true);
  };
  const changeRow = (key: number, patch: Partial<FieldRow>) =>
    change({ fields: form.fields.map((row) => (row.key === key ? { ...row, ...patch } : row)) });

  const switchView = (next: 'form' | 'yaml') => {
    if (next === view) return;
    if ((view === 'form' ? formDirty : yamlDirty) && !window.confirm(T.unsavedChanges)) return;
    setView(next);
  };

  const save = async () => {
    setNotice(null);
    const body = view === 'form' ? { data: formBody(form) } : { content: yaml };
    try {
      const response = await sendJson('/api/admin/config', 'PUT', body);
      const data = await readJson<{
        message?: string;
        error?: string;
        errors?: string[];
        config_content?: string;
        config_data?: ConfigFormData;
      }>(response);
      if (response.ok) {
        setNotice({ type: 'success', text: data.message ?? '' });
        // Update both views from the answer
        if (data.config_content !== undefined) setYaml(data.config_content);
        if (data.config_data) setForm(formFromData(data.config_data, nextKey));
        setFormDirty(false);
        setYamlDirty(false);
      } else {
        setNotice({ type: 'error', text: data.error || T.failedToSaveConfig, errors: data.errors });
      }
    } catch (error) {
      setNotice({ type: 'error', text: T.error((error as Error).message) });
    }
  };

  const toggleClass = (active: boolean) =>
    'cursor-pointer border border-[var(--border)] px-4 py-1.5 ' +
    (active ? 'bg-blue-600 text-white' : 'bg-[var(--card)] text-[var(--fg)]');

  return (
    <div id="config-tab">
      <div className="my-3 font-bold">
        {T.configFile} <span id="config-file-path" className="font-normal opacity-70">{props.path}</span>
      </div>

      <div className="config-view-toggle mb-3 flex">
        <button type="button" data-view="form" className={toggleClass(view === 'form') + ' rounded-l'} onClick={() => switchView('form')}>
          {T.formView}
        </button>
        <button type="button" data-view="yaml" className={toggleClass(view === 'yaml') + ' rounded-r'} onClick={() => switchView('yaml')}>
          {T.yamlView}
        </button>
      </div>

      {view === 'form' ? (
        <div id="config-form-view">
          <section className="mb-4 rounded border border-[var(--border)] p-3">
            <h3 className="mb-2 mt-0 font-bold">{T.general}</h3>
            <div className="mb-3">
              <label htmlFor="cfg-language" className="mb-1 block font-bold">
                {T.language}
              </label>
              <select
                id="cfg-language"
                value={form.language}
                onChange={(event) => change({ language: event.target.value as FormState['language'] })}
                className={inputClass}
              >
                <option value="uk">{T.langUk}</option>
                <option value="en">{T.langEn}</option>
                <option value="ru">{T.langRu}</option>
              </select>
              <div className="mt-1 text-xs opacity-70">{T.languageNote}</div>
            </div>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                id="cfg-extra-answers-with-users-csv"
                checked={form.extraCsv}
                onChange={(event) => change({ extraCsv: event.target.checked })}
              />
              {T.extraCsv}
            </label>
            <div className="ml-6 mt-1 text-xs opacity-70">{T.extraCsvNote}</div>
          </section>

          <section className="mb-4 rounded border border-[var(--border)] p-3">
            <h3 className="mb-2 mt-0 font-bold">{T.registration}</h3>
            <label className="mb-3 flex items-center gap-2">
              <input type="checkbox" id="cfg-reg-approve" checked={form.approve} onChange={(event) => change({ approve: event.target.checked })} />
              {T.approve}
            </label>
            <div className="mb-3">
              <label htmlFor="cfg-reg-username-label" className="mb-1 block font-bold">
                {T.usernameLabel}
              </label>
              <input
                type="text"
                id="cfg-reg-username-label"
                value={form.usernameLabel}
                placeholder={T.usernameLabelPlaceholder}
                onChange={(event) => change({ usernameLabel: event.target.value })}
                className={inputClass + ' w-full max-w-md'}
              />
            </div>
            <div>
              <div className="font-bold">{T.extraFields}</div>
              <div className="mb-2 text-xs opacity-70">{T.extraFieldsNote}</div>
              <div id="cfg-reg-fields-list" className="flex flex-col gap-2">
                {form.fields.map((row) => (
                  <div key={row.key} className={'config-list-item flex items-center gap-2' + (row.enabled ? '' : ' disabled opacity-60')}>
                    <input
                      type="checkbox"
                      title={T.toggleField}
                      checked={row.enabled}
                      onChange={(event) => changeRow(row.key, { enabled: event.target.checked })}
                    />
                    <input
                      type="text"
                      value={row.name}
                      placeholder={T.fieldName}
                      onChange={(event) => changeRow(row.key, { name: event.target.value })}
                      className={inputClass + ' min-w-0 flex-1'}
                    />
                    <button
                      type="button"
                      className="config-list-remove-btn cursor-pointer rounded bg-red-600 px-2 py-1 text-white"
                      title={T.removeField}
                      onClick={() => change({ fields: form.fields.filter((item) => item.key !== row.key) })}
                    >
                      {'✕'}
                    </button>
                  </div>
                ))}
              </div>
              <button
                type="button"
                className="config-list-add-btn mt-2 cursor-pointer rounded border border-[var(--border)] px-3 py-1"
                onClick={() => change({ fields: [...form.fields, { key: nextKey(), name: '', enabled: true }] })}
              >
                {T.addField}
              </button>
            </div>
          </section>
        </div>
      ) : (
        <div id="config-yaml-view">
          <textarea
            id="config-editor"
            rows={25}
            value={yaml}
            onChange={(event) => {
              setYaml(event.target.value);
              setYamlDirty(true);
            }}
            className={inputClass + ' box-border w-full font-mono text-sm'}
          />
        </div>
      )}

      <div className="my-3">
        <button type="button" className={primaryButton + ' save-config-btn'} onClick={() => void save()}>
          {T.saveConfig}
        </button>
      </div>
      <div id="config-message-area">
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
    </div>
  );
}
