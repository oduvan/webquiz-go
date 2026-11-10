import { describe, expect, it } from 'vitest';
import { transferChoiceFor } from './Panels';
import type { TransferFile } from './types';
import { documentationURL, isNewer, latestReleaseURL, releasePageURL, versionFromTag } from './version';

describe('new version check', () => {
  it('asks the GitHub releases of the repository from the boot data', () => {
    expect(latestReleaseURL('https://github.com/oduvan/webquiz-go')).toBe(
      'https://api.github.com/repos/oduvan/webquiz-go/releases/latest',
    );
    expect(latestReleaseURL('https://github.com/oduvan/webquiz-go/')).toBe(
      'https://api.github.com/repos/oduvan/webquiz-go/releases/latest',
    );
    expect(latestReleaseURL('')).toBeNull();
    expect(latestReleaseURL('https://example.com/x/y')).toBeNull();
  });

  it('compares versions part by part', () => {
    expect(isNewer('2.1', '2.0')).toBe(true);
    expect(isNewer('2.10', '2.9')).toBe(true);
    expect(isNewer('2.0.1', '2.0')).toBe(true);
    expect(isNewer('2.0', '2.0')).toBe(false);
    expect(isNewer('2.0', '2.0.0')).toBe(false);
    expect(isNewer('1.19', '2.0')).toBe(false);
  });

  it('builds the release and documentation addresses', () => {
    expect(versionFromTag('v2.1')).toBe('2.1');
    expect(versionFromTag('2.1')).toBe('2.1');
    expect(releasePageURL('https://github.com/oduvan/webquiz-go', 'v2.1')).toBe(
      'https://github.com/oduvan/webquiz-go/releases/tag/v2.1',
    );
    expect(documentationURL('https://github.com/oduvan/webquiz-go', '2.0')).toBe(
      'https://github.com/oduvan/webquiz-go/releases/download/v2.0/webquiz-documentation-uk.pdf',
    );
  });
});

describe('transfer choices', () => {
  const file: TransferFile = {
    type: 'quizzes',
    name: 'a.yaml',
    size: 10,
    target: 'a.yaml',
    case: 'different',
    actions: ['replace', 'rename', 'skip'],
    default_action: 'rename',
    suggested_name: 'srv_a.yaml',
  };

  it('starts with the server suggestion', () => {
    expect(transferChoiceFor(undefined, file)).toEqual({ action: 'rename', new_name: 'srv_a.yaml' });
  });

  it('keeps the admin choice', () => {
    expect(transferChoiceFor({ 'quizzes/a.yaml': { action: 'skip', new_name: 'x' } }, file)).toEqual({
      action: 'skip',
      new_name: 'x',
    });
  });

  it('falls back to the first action offered', () => {
    expect(transferChoiceFor(undefined, { ...file, default_action: 'save' }).action).toBe('replace');
  });
});
