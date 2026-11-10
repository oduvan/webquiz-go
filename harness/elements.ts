// What the teacher tool shows: the numbered list of things on the screen a
// person can use, and the text of one teacher step.
//
// The list is made in the page by COLLECT_JS (it also marks every listed
// element with data-harness-n, so "click 5" finds element 5 again). The
// formatting functions are pure, so they are tested without a browser.

export type ElementKind =
  | 'button'
  | 'link'
  | 'input'
  | 'textarea'
  | 'checkbox'
  | 'radio'
  | 'select'
  | 'tab'
  | 'option'
  | 'clickable'
  | 'file';

/** One element of the list. */
export interface ElementInfo {
  n: number;
  kind: ElementKind;
  label: string;
  /** Text fields: the text in them. Selects: the chosen option(s). */
  value?: string | null;
  /** Checkboxes and radio buttons. */
  checked?: boolean;
  /** Selects: the text of every option. */
  options?: string[];
  /** Selects where several options can be chosen. */
  multiple?: boolean;
  disabled?: boolean;
  /** A tab or button that is the current choice, or a chosen answer option. */
  active?: boolean;
  /** Links: where they go. */
  href?: string;
  /** For labels without words (like "📋"): the element's title, id or class. */
  hint?: string;
  hintKind?: 'title' | 'id' | 'class';
  /** For labels that repeat: the text around the element, to tell them apart. */
  near?: string;
  /** "number" or "password" for such inputs. */
  inputType?: string;
}

/** One step of a teacher, as the driver records it in events.jsonl. */
export interface TeacherStepRecord {
  step: number;
  t: number;
  t_start?: number;
  student?: string;
  role?: string;
  cmd?: string;
  args?: string[];
  arg?: string | null;
  result: string | null;
  error?: string | null;
  /** Dialogs, downloads and new tabs during the command, already as text. */
  events?: string[];
  url?: string | null;
  tab?: number;
  tabs?: number;
  screen: string;
  elements: ElementInfo[];
  screenshot?: string | null;
}

const MAX_VALUE_LINES = 60;
const MAX_VALUE_CHARS = 3000;
const MAX_OPTIONS = 40;

function quote(text: string): string {
  return `"${text}"`;
}

/** A value with line breaks (or a long one) is shown on its own lines under the element. */
function isLongValue(value: string): boolean {
  return value.includes('\n') || value.length > 120;
}

/** The lines of a long value, each starting with "    | ". */
export function valueBlock(value: string): string[] {
  let text = value;
  let cut = false;
  if (text.length > MAX_VALUE_CHARS) {
    text = text.slice(0, MAX_VALUE_CHARS);
    cut = true;
  }
  let lines = text.split('\n');
  if (lines.length > MAX_VALUE_LINES) {
    lines = lines.slice(0, MAX_VALUE_LINES);
    cut = true;
  }
  const block = lines.map((line) => `    | ${line}`);
  if (cut) block.push(`    | … (the rest is not shown: ${value.split('\n').length} lines in all)`);
  return block;
}

/** One element as text, for example: [9] checkbox "Показувати правильні відповіді" = on */
export function formatElement(element: ElementInfo): string {
  const parts = [`[${element.n}] ${element.kind} ${quote(element.label)}`];
  const extra: string[] = [];
  let block: string[] = [];
  switch (element.kind) {
    case 'input':
    case 'textarea': {
      const value = element.value ?? '';
      if (isLongValue(value)) {
        parts.push(`= (${value.split('\n').length} lines, shown below)`);
        block = valueBlock(value);
      } else {
        parts.push(`= ${quote(value)}`);
      }
      if (element.inputType && element.inputType !== 'text') extra.push(`${element.inputType} field`);
      break;
    }
    case 'checkbox':
    case 'radio':
      parts.push(`= ${element.checked ? 'on' : 'off'}`);
      break;
    case 'select': {
      const options = element.options ?? [];
      const shown = options.slice(0, MAX_OPTIONS).map(quote);
      if (options.length > MAX_OPTIONS) shown.push(`… ${options.length - MAX_OPTIONS} more`);
      if (element.multiple) {
        const chosen = element.value ? element.value.split('\n').filter(Boolean) : [];
        parts.push(`= ${chosen.length ? chosen.map(quote).join(', ') : 'nothing chosen'}`);
        extra.push(`several can be chosen; options: ${shown.join(', ')}`);
      } else {
        parts.push(`= ${quote(element.value ?? '')}`);
        extra.push(`options: ${shown.join(', ')}`);
      }
      break;
    }
    case 'link':
      if (element.href) parts.push(`-> ${element.href}`);
      break;
    default:
      break;
  }
  if (element.active) extra.push(element.kind === 'option' ? 'chosen' : 'selected');
  if (element.disabled) extra.push('disabled');
  if (element.hint) {
    const kind = element.hintKind ?? 'title';
    extra.push(kind === 'title' ? `title: ${quote(element.hint)}` : `${kind}: ${element.hint}`);
  }
  if (element.near) extra.push(`at: ${element.near}`);
  let line = parts.join(' ');
  if (extra.length) line += ` (${extra.join('; ')})`;
  return [line, ...block].join('\n');
}

/** The text a teacher agent gets back for one step. */
export function formatTeacherStep(record: TeacherStepRecord): string {
  const lines = [`STEP ${record.step}  (time +${record.t.toFixed(1)} s)`];
  if (record.url) {
    const where = record.tabs && record.tabs > 1 ? `  (tab ${record.tab} of ${record.tabs})` : '';
    lines.push(`PAGE: ${record.url}${where}`);
  } else {
    lines.push('PAGE: no tab is open (use: open admin, open files, open live-stats or open quiz)');
  }
  lines.push(`RESULT: ${record.result || ''}` + (record.error ? `  ERROR: ${record.error}` : ''));
  for (const event of record.events ?? []) lines.push(event);
  lines.push('SCREEN:');
  lines.push(record.screen);
  if (record.elements.length) {
    lines.push('ELEMENTS (use these numbers in your next command):');
    for (const element of record.elements) lines.push(formatElement(element));
  } else {
    lines.push('ELEMENTS: none');
  }
  return lines.join('\n');
}

export interface SelectOption {
  value: string;
  label: string;
}

/**
 * Find the option a teacher means: the exact text, the exact value, then text that
 * starts with or contains the words (only when one option matches).
 */
export function matchOption(options: SelectOption[], wanted: string): SelectOption | string {
  const want = wanted.trim().toLowerCase();
  const norm = (text: string) => text.replace(/\s+/g, ' ').trim().toLowerCase();
  const tries: ((option: SelectOption) => boolean)[] = [
    (option) => norm(option.label) === want,
    (option) => norm(option.value) === want,
    (option) => norm(option.label).startsWith(want),
    (option) => norm(option.label).includes(want) || norm(option.value).includes(want),
  ];
  for (const test of tries) {
    const found = options.filter(test);
    if (found.length === 1) return found[0];
    if (found.length > 1) {
      return `"${wanted}" fits ${found.length} options (${found.map((o) => quote(o.label)).join(', ')}); write more of the text`;
    }
  }
  return `no option "${wanted}"; the options are: ${options.map((o) => quote(o.label)).join(', ')}`;
}

/** Replace `old` with `replacement` once in `value`. Returns the new value or an error text. */
export function replaceOnce(value: string, old: string, replacement: string): { value: string } | { error: string } {
  if (!old) return { error: 'the text to replace is empty' };
  const at = value.indexOf(old);
  if (at === -1) return { error: `the field does not contain "${old}"` };
  return { value: value.slice(0, at) + replacement + value.slice(at + old.length) };
}

/** "\n" written by an agent in a command means a new line. */
export function unescapeNewlines(text: string): string {
  return text.replace(/\\n/g, '\n').replace(/\\t/g, '\t');
}

// ---- In the page ----

/**
 * Lists what a person can use on the page and marks it with data-harness-n.
 *
 * Takes {start}: the first number to use. Returns {text, items, count, overlay, frames}.
 * When a pop-up covers the page, only the pop-up is listed (and the messages above it).
 * Passed to the page as text, so no build tool can change the function.
 */
export const COLLECT_JS = String.raw`(options) => {
  const start = options && options.start ? options.start : 1;
  const clean = (s) => String(s == null ? '' : s).replace(/\s+/g, ' ').trim();
  const hasWord = (s) => /[\p{L}\p{N}]/u.test(s);
  const visible = (el) => {
    if (!el || !el.getClientRects || el.getClientRects().length === 0) return false;
    const style = getComputedStyle(el);
    return style.visibility !== 'hidden' && style.display !== 'none';
  };
  document.querySelectorAll('[data-harness-n]').forEach((el) => el.removeAttribute('data-harness-n'));
  document.querySelectorAll('[data-harness-frame]').forEach((el) => el.removeAttribute('data-harness-frame'));
  const body = document.body;
  if (!body) return { text: '', items: [], count: 0, overlay: false, frames: 0 };

  // A pop-up: a fixed box that covers (almost) the whole window. The top one wins.
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  let overlay = null;
  let overlayZ = -Infinity;
  for (const el of body.getElementsByTagName('*')) {
    const style = getComputedStyle(el);
    if (style.position !== 'fixed' || style.display === 'none' || style.visibility === 'hidden') continue;
    if (style.pointerEvents === 'none') continue;
    const rect = el.getBoundingClientRect();
    if (rect.width < vw * 0.9 || rect.height < vh * 0.9) continue;
    const z = parseInt(style.zIndex, 10);
    const zz = isNaN(z) ? 0 : z;
    if (zz >= overlayZ) {
      overlay = el;
      overlayZ = zz;
    }
  }
  const root = overlay || body;

  const SEL = 'a[href], button, input, textarea, select, summary, [role="button"], [role="tab"], [role="checkbox"], [role="radio"], [role="link"], [role="menuitem"], [role="option"], [contenteditable="true"], [contenteditable=""]';
  const found = new Set();
  root.querySelectorAll(SEL).forEach((el) => found.add(el));
  const pointer = (el) => getComputedStyle(el).cursor === 'pointer';
  for (const el of root.getElementsByTagName('*')) {
    if (found.has(el)) continue;
    const tag = el.tagName.toUpperCase();
    if (['LABEL', 'OPTION', 'OPTGROUP', 'SVG', 'PATH', 'HTML', 'BODY'].includes(tag)) continue;
    if (!pointer(el)) continue;
    const parent = el.parentElement;
    if (parent && pointer(parent)) continue;
    if (el.closest('label') || el.closest(SEL)) continue;
    found.add(el);
  }
  if (overlay && pointer(overlay) && !overlay.matches(SEL)) found.add(overlay);

  const list = [...found].filter((el) => {
    if (!visible(el)) return false;
    if (el.tagName === 'INPUT' && (el.getAttribute('type') || '').toLowerCase() === 'hidden') return false;
    return true;
  });
  list.sort((a, b) => (a === b ? 0 : a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1));
  const listed = new Set(list);

  const imageName = (img) => {
    const src = img.getAttribute('src') || '';
    let name = src.split('?')[0].split('/').pop() || '';
    try { name = decodeURIComponent(name); } catch (e) { /* keep it */ }
    return name;
  };
  // The text of an element without the text of the listed elements inside it
  const ownText = (el) => {
    let text = '';
    const walk = (node) => {
      for (const child of node.childNodes) {
        if (child.nodeType === 3) {
          text += child.textContent;
        } else if (child.nodeType === 1) {
          if (child !== el && listed.has(child)) { text += ' '; continue; }
          const tag = child.tagName.toUpperCase();
          if (['SCRIPT', 'STYLE', 'TEMPLATE'].includes(tag)) continue;
          const style = getComputedStyle(child);
          if (style.display === 'none' || style.visibility === 'hidden') continue;
          if (tag === 'IMG') {
            const alt = clean(child.getAttribute('alt'));
            text += ' ' + (alt && /[\p{L}]/u.test(alt) && !/^(preview|option preview)$/i.test(alt) ? alt : '[image ' + imageName(child) + ']') + ' ';
            continue;
          }
          text += ' ';
          walk(child);
          text += ' ';
        }
      }
    };
    walk(el);
    return clean(text);
  };
  const labelsText = (el) => {
    if (!el.labels || !el.labels.length) return '';
    return clean([...el.labels].map((label) => {
      let text = '';
      const walk = (node) => {
        for (const child of node.childNodes) {
          if (child.nodeType === 3) text += child.textContent;
          else if (child.nodeType === 1 && !child.matches('input, select, textarea, button')) { text += ' '; walk(child); }
        }
      };
      walk(label);
      return text;
    }).join(' '));
  };
  const attr = (el, name) => clean(el.getAttribute(name));
  const hasControl = (el) => el.matches(SEL) || !!el.querySelector(SEL);
  // A label or a short text just before the element (up to three levels up)
  const nearbyLabel = (el) => {
    let node = el;
    for (let level = 0; level < 3 && node && node !== body && node !== root; level++) {
      let sibling = node.previousElementSibling;
      let seen = 0;
      while (sibling && seen < 4) {
        if (visible(sibling)) {
          const text = clean(sibling.innerText);
          if (sibling.tagName.toUpperCase() === 'LABEL' && text) return text;
          if (!hasControl(sibling) && text && text.length <= 100) return text;
        }
        sibling = sibling.previousElementSibling;
        seen++;
      }
      node = node.parentElement;
    }
    return '';
  };
  const nextText = (el) => {
    let next = el.nextSibling;
    while (next && next.nodeType === 3 && !clean(next.textContent)) next = next.nextSibling;
    if (!next) return '';
    if (next.nodeType === 3) return clean(next.textContent).slice(0, 80);
    if (next.nodeType !== 1) return '';
    const tag = next.tagName.toUpperCase();
    if (tag === 'INPUT' || tag === 'TEXTAREA') return (clean(next.value) || attr(next, 'placeholder')).slice(0, 80);
    if (tag === 'SELECT' || tag === 'BUTTON') return '';
    return clean(next.innerText).slice(0, 80);
  };
  // A class that names what the element does (not a style class like "px-4")
  const STYLE_CLASS = /^(cursor|rounded|bg|text|px|py|pt|pb|pl|pr|p|m|mx|my|mt|mb|ml|mr|w|h|min|max|flex|grid|gap|border|font|leading|whitespace|hover|disabled|dark|md|shadow|opacity|no|inline|block|items|justify|overflow|sticky|top|left|right|bottom|z|transition|break|order|basis|self|space|hidden|active|selected)(-|$)/;
  const meaningfulClass = (el) => [...el.classList].find((c) => /[a-z]/.test(c) && !c.includes(':') && !c.includes('[') && !STYLE_CLASS.test(c)) || '';

  const kindOf = (el) => {
    const tag = el.tagName.toUpperCase();
    const type = (el.getAttribute('type') || '').toLowerCase();
    const role = el.getAttribute('role');
    if (tag === 'SELECT') return 'select';
    if (tag === 'TEXTAREA') return 'textarea';
    if (tag === 'INPUT') {
      if (type === 'checkbox') return 'checkbox';
      if (type === 'radio') return 'radio';
      if (['button', 'submit', 'reset', 'image'].includes(type)) return 'button';
      if (type === 'file') return 'file';
      return 'input';
    }
    if (el.classList.contains('option') || el.classList.contains('quiz-option')) return 'option';
    if (role === 'tab' || el.classList.contains('tab') || el.hasAttribute('data-tab') || (tag === 'BUTTON' && el.hasAttribute('data-view'))) return 'tab';
    if (role === 'checkbox') return 'checkbox';
    if (role === 'radio') return 'radio';
    if (tag === 'A' || role === 'link') return 'link';
    if (tag === 'BUTTON' || role === 'button' || tag === 'SUMMARY') return 'button';
    if (el.isContentEditable) return 'textarea';
    return 'clickable';
  };

  const items = list.map((el, index) => {
    const kind = kindOf(el);
    const item = { n: start + index, kind, label: '' };
    const aria = attr(el, 'aria-label');
    if (kind === 'input' || kind === 'textarea' || kind === 'select' || kind === 'file') {
      item.label = aria || labelsText(el) || attr(el, 'placeholder') || nearbyLabel(el) || attr(el, 'title');
    } else if ((kind === 'checkbox' || kind === 'radio') && el.tagName.toUpperCase() === 'INPUT') {
      item.label = aria || labelsText(el) || nextText(el) || nearbyLabel(el) || attr(el, 'title');
    } else if (el.tagName.toUpperCase() === 'INPUT') {
      item.label = clean(el.value) || aria || attr(el, 'title');
    } else {
      item.label = ownText(el) || aria || attr(el, 'title');
    }
    if (kind === 'clickable' && hasWord(item.label)) {
      // "☰ ▼ 1. Question" -> "1. Question": signs without words say nothing
      item.label = item.label.split(' ').filter((part) => hasWord(part)).join(' ');
    }
    if (!hasWord(item.label)) {
      const title = attr(el, 'title') || (aria !== item.label ? aria : '');
      if (title && title !== item.label) {
        item.hint = title;
        item.hintKind = 'title';
      } else if (el.id) {
        item.hint = el.id;
        item.hintKind = 'id';
      } else if (meaningfulClass(el)) {
        item.hint = meaningfulClass(el);
        item.hintKind = 'class';
      }
    }
    if (kind === 'input' || kind === 'textarea') {
      item.value = el.isContentEditable && kind === 'textarea' && el.tagName.toUpperCase() !== 'TEXTAREA' ? el.innerText : el.value;
      const type = (el.getAttribute('type') || '').toLowerCase();
      if (type && type !== 'text') item.inputType = type;
    } else if (kind === 'select') {
      item.options = [...el.options].map((o) => clean(o.label || o.text));
      item.multiple = !!el.multiple;
      item.value = [...el.options].filter((o) => o.selected).map((o) => clean(o.label || o.text)).join('\n');
    } else if (kind === 'checkbox' || kind === 'radio') {
      item.checked = el.tagName.toUpperCase() === 'INPUT' ? !!el.checked : el.getAttribute('aria-checked') === 'true';
    } else if (kind === 'link') {
      const href = el.getAttribute('href') || '';
      try {
        const url = new URL(href, location.href);
        item.href = url.origin === location.origin ? url.pathname + url.search : url.href;
      } catch (e) {
        item.href = href;
      }
      if (el.getAttribute('target') === '_blank') item.href += '  (opens a new tab)';
    }
    if (kind === 'option') item.active = el.classList.contains('selected') || el.getAttribute('aria-checked') === 'true';
    else if (kind === 'tab' || kind === 'button') {
      item.active = el.classList.contains('active') || el.getAttribute('aria-selected') === 'true' || el.getAttribute('aria-pressed') === 'true';
    }
    if (!item.active) delete item.active;
    if (el.disabled || el.getAttribute('aria-disabled') === 'true' || el.classList.contains('disabled')) item.disabled = true;
    return item;
  });

  // The text inside a box, with what is typed in its fields, without one element
  const summary = (box, skip) => {
    const parts = [];
    const walk = (node) => {
      if (parts.join(' ').length > 160) return;
      if (node === skip) return;
      if (node.nodeType === 3) { const t = clean(node.textContent); if (t) parts.push(t); return; }
      if (node.nodeType !== 1) return;
      const tag = node.tagName.toUpperCase();
      if (['SCRIPT', 'STYLE', 'TEMPLATE', 'OPTION'].includes(tag)) return;
      if (!visible(node)) return;
      if (tag === 'INPUT') {
        const type = (node.getAttribute('type') || 'text').toLowerCase();
        if (['text', 'number', 'email', 'search', 'url', 'tel', 'password'].includes(type)) {
          if (node.value) parts.push('"' + clean(node.value).slice(0, 40) + '"');
          else if (attr(node, 'placeholder')) parts.push('(empty ' + attr(node, 'placeholder').slice(0, 30) + ')');
        }
        return;
      }
      if (tag === 'TEXTAREA') { if (node.value) parts.push('"' + clean(node.value).slice(0, 30) + '"'); return; }
      if (tag === 'SELECT') { const o = node.options[node.selectedIndex]; if (o) parts.push(clean(o.text)); return; }
      if (tag === 'BUTTON') return;
      for (const child of node.childNodes) walk(child);
    };
    walk(box);
    let text = clean(parts.filter((p) => hasWord(p)).join(' '));
    if (text.length > 60) text = text.slice(0, 59) + '…';
    return text;
  };
  // A box is named by its first part that has text (a row's header, a field's label)
  const boxName = (box, skip) => {
    for (const child of box.children) {
      if (child === skip) continue;
      const text = summary(child, skip);
      if (text) return text;
    }
    return summary(box, skip);
  };
  const groups = new Map();
  items.forEach((item, index) => {
    const key = item.kind + '\u0000' + item.label;
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(index);
  });
  for (const indexes of groups.values()) {
    const unnamed = !hasWord(items[indexes[0]].label) && !items[indexes[0]].hint;
    if (indexes.length < 2 && !unnamed) continue;
    const twins = indexes.map((i) => list[i]);
    indexes.forEach((i, k) => {
      const el = list[i];
      let box = el;
      while (box.parentElement && box.parentElement !== body && box.parentElement !== root &&
             !twins.some((other) => other !== el && box.parentElement.contains(other))) {
        box = box.parentElement;
      }
      if (indexes.length === 1 && box !== el) box = el.parentElement || box;
      let near = box === el ? '' : boxName(box, el);
      if (!near && indexes.length > 1) near = 'number ' + (k + 1) + ' of ' + indexes.length + ' with this name';
      if (near) items[i].near = near;
    });
  }

  list.forEach((el, index) => el.setAttribute('data-harness-n', String(start + index)));

  // Frames inside what is shown get a mark, so their content can be listed too
  let frames = 0;
  root.querySelectorAll('iframe').forEach((frame) => {
    if (visible(frame)) frame.setAttribute('data-harness-frame', String(++frames));
  });

  // The screen text: the pop-up (and the messages above it), or the whole page
  let text;
  if (overlay) {
    const pieces = [];
    const area = document.getElementById('message-area');
    const messages = [];
    if (area && !overlay.contains(area) && visible(area)) messages.push(area);
    document.querySelectorAll('[role="alert"], [role="status"]').forEach((el) => {
      if (!overlay.contains(el) && visible(el) && !messages.some((m) => m.contains(el))) messages.push(el);
    });
    messages.forEach((m) => { const t = (m.innerText || '').trim(); if (t) pieces.push(t); });
    pieces.push('[a pop-up window is open; only the pop-up is shown]');
    pieces.push(overlay.innerText || '');
    text = pieces.join('\n');
  } else {
    text = body.innerText || '';
  }
  text = text.replace(/\n{3,}/g, '\n\n').trim();

  return { text, items, count: items.length, overlay: !!overlay, frames };
}`;

/**
 * Is the page wider than the window (a sideways scroll bar)? Returns null when it fits,
 * else the widths and up to three elements that stick out.
 */
export const WIDE_JS = String.raw`() => {
  const body = document.body;
  const doc = document.scrollingElement || document.documentElement;
  if (!body || !doc || doc.scrollWidth <= doc.clientWidth + 1) return null;
  const culprits = [];
  for (const el of body.getElementsByTagName('*')) {
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.right + window.scrollX <= doc.clientWidth + 1) continue;
    const parent = el.parentElement;
    if (parent && parent !== body && parent.getBoundingClientRect().right + window.scrollX > doc.clientWidth + 1) continue;
    const classes = [...el.classList].slice(0, 2).map((c) => '.' + c).join('');
    culprits.push(el.tagName.toLowerCase() + (el.id ? '#' + el.id : '') + classes + ' (' + Math.round(rect.width) + ' px)');
    if (culprits.length >= 3) break;
  }
  return { page: doc.scrollWidth, view: doc.clientWidth, culprits };
}`;
