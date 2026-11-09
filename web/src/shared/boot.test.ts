import { afterEach, describe, expect, it } from 'vitest';
import { readBoot, type QuizBoot } from './boot';

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
  it('reads a quiz page boot as the Go server writes it', () => {
    setBoot(
      '{"state":"quiz","language":"en","translations":{"submit_answer":"Submit answer"},"version":"2.0",' +
        '"title":"A \\u003cb\\u003e quiz","questions":[' +
        '{"id":1,"is_multiple_choice":false,"options":["3","4"],"points":1,"question":"2+2?","time_limit":0},' +
        '{"default_value":"start","id":2,"points":0.5,"time_limit":30,"type":"text"}],' +
        '"username_label":"Username","registration_fields":[{"label":"Group","name":"group"}],' +
        '"show_right_answer":true,"show_final_list":true,"quiz_time_limit":0,"quizzes":[]}',
    );
    const boot = readBoot<QuizBoot | null>(null);
    expect(boot?.title).toBe('A <b> quiz');
    const kinds = (boot?.questions ?? []).map((q) =>
      q.type === 'text' ? 'text:' + String(q.default_value) : 'choice:' + q.options.length,
    );
    expect(kinds).toEqual(['choice:2', 'text:start']);
  });
});
