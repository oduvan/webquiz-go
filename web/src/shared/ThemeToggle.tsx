import { useEffect, useState } from 'react';
import { getTheme, setTheme, type Theme } from './theme';

export function ThemeToggle({ title = 'Theme' }: { title?: string }) {
  const [theme, setState] = useState<Theme>(getTheme());

  useEffect(() => {
    const onStorage = () => setState(getTheme());
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);

  const toggle = () => {
    const next: Theme = theme === 'dark' ? 'light' : 'dark';
    setTheme(next);
    setState(next);
  };

  return (
    <button type="button" className="theme-toggle" title={title} onClick={toggle}>
      {theme === 'dark' ? '☀️' : '🌙'}
    </button>
  );
}
