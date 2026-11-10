export type Theme = 'light' | 'dark';

const KEY = 'theme';

export function getTheme(): Theme {
  try {
    return localStorage.getItem(KEY) === 'dark' ? 'dark' : 'light';
  } catch (e) {
    return 'light';
  }
}

export function applyTheme(theme: Theme) {
  if (theme === 'dark') {
    document.documentElement.setAttribute('data-theme', 'dark');
  } else {
    document.documentElement.removeAttribute('data-theme');
  }
}

export function setTheme(theme: Theme) {
  try {
    localStorage.setItem(KEY, theme);
  } catch (e) {
    // Storage may be blocked. The theme still changes for this page.
  }
  applyTheme(theme);
}

// Applies the saved theme and follows changes made in other tabs and pages.
export function initTheme(): () => void {
  applyTheme(getTheme());
  const onStorage = (event: StorageEvent) => {
    if (event.key === KEY || event.key === null) applyTheme(getTheme());
  };
  window.addEventListener('storage', onStorage);
  return () => window.removeEventListener('storage', onStorage);
}
