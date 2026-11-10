// How a recorded step is shown as text.
//
// This module does not need Playwright, so the checks (and their tests) can use it
// on a machine where no browser is installed.

export interface Clickable {
  kind: string;
  label: string;
  disabled: boolean;
}

export interface InputField {
  kind: string;
  label: string;
  value: string;
  disabled: boolean;
}

export interface StepRecord {
  step: number;
  t: number;
  t_start?: number;
  student?: string;
  cmd?: string;
  arg?: string | null;
  result: string | null;
  error?: string | null;
  screen: string;
  clickable: Clickable[];
  inputs: InputField[];
  screenshot?: string | null;
}

/** The text an agent gets back for a step. */
export function formatStep(record: StepRecord): string {
  const lines = [`STEP ${record.step}  (time +${record.t.toFixed(1)} s)`];
  lines.push(`RESULT: ${record.result || ''}` + (record.error ? `  ERROR: ${record.error}` : ''));
  lines.push('SCREEN:');
  lines.push(record.screen);
  if (record.clickable.length) {
    const items = record.clickable.map((c) => `[${c.kind}] ${c.label}` + (c.disabled ? ' (disabled)' : ''));
    lines.push('CLICKABLE: ' + items.join(' · '));
  }
  if (record.inputs.length) {
    const items = record.inputs.map((i) => `[${i.kind}] ${i.label} = "${i.value}"` + (i.disabled ? ' (disabled)' : ''));
    lines.push('TYPE INTO: ' + items.join(' · '));
  }
  return lines.join('\n');
}
