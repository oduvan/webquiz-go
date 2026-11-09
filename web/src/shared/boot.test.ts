import { afterEach, describe, expect, it } from 'vitest';
import { readBoot } from './boot';

function setBoot(text: string | null) {
  document.body.innerHTML = '';
  if (text === null) return;
  const el = document.createElement('script');
  el.id = 'webquiz-boot';
  el.type = 'application/json';
  el.textContent = text;
  document.body.appendChild(el);
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('readBoot', () => {
  it('parses the boot JSON', () => {
    setBoot('{"title":"Quiz","n":3}');
    expect(readBoot({ title: '', n: 0 })).toEqual({ title: 'Quiz', n: 3 });
  });
  it('reads escaped angle brackets back as characters', () => {
    setBoot('{"t":"\\u003c/script\\u003e \\u0026"}');
    expect(readBoot({ t: '' })).toEqual({ t: '</script> &' });
  });
  it('returns the fallback while the marker is still raw', () => {
    setBoot('__WEBQUIZ_BOOT__');
    expect(readBoot({ dev: true })).toEqual({ dev: true });
  });
  it('parses boot JSON that contains the marker text', () => {
    setBoot('{"note":"__WEBQUIZ_BOOT__"}');
    expect(readBoot({ note: '' })).toEqual({ note: '__WEBQUIZ_BOOT__' });
  });
  it('returns the fallback when the script is missing or empty', () => {
    setBoot(null);
    expect(readBoot({ dev: true })).toEqual({ dev: true });
    setBoot('   ');
    expect(readBoot({ dev: true })).toEqual({ dev: true });
  });
  it('throws on broken JSON', () => {
    setBoot('{oops');
    expect(() => readBoot({})).toThrow();
  });
});
