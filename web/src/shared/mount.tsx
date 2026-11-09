import { StrictMode, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import '../index.css';
import { initTheme } from './theme';
import { ThemeToggle } from './ThemeToggle';

// Every page starts here: theme, theme button, then the page itself.
export function mountPage(page: ReactNode) {
  initTheme();
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <ThemeToggle />
      {page}
    </StrictMode>,
  );
}
