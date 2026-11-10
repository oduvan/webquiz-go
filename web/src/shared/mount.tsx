import { StrictMode, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import '../index.css';
import { initTheme } from './theme';
import { ThemeToggle } from './ThemeToggle';

export interface MountOptions {
  // The tooltip of the theme button. The quiz page passes its translated text.
  themeTitle?: string;
}

// Every page starts here: theme, theme button, then the page itself.
export function mountPage(page: ReactNode, options: MountOptions = {}) {
  initTheme();
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <ThemeToggle title={options.themeTitle} />
      {page}
    </StrictMode>,
  );
}
