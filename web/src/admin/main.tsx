import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '../index.css';
import './admin.css';
import { readBoot, type AdminBoot } from '../shared/boot';
import { initTheme } from '../shared/theme';
import { ThemeToggle } from '../shared/ThemeToggle';
import { AdminApp } from './AdminApp';
import { MessageProvider } from './messages';
import { T } from './texts';

// In the Vite dev server the page has no boot data: these values stand in.
function devBoot(): AdminBoot {
  return {
    is_trusted_ip: false,
    network_info: { urls: [{ label: 'Network Access (192.168.1.10)', quiz_url: 'http://192.168.1.10:8080/' }] },
    downloadable_quizzes: [],
    version: 'dev',
    tunnel_public_key: '',
    repo_url: '',
    pages_url: '',
  };
}

export function startAdminPage() {
  const boot = readBoot<AdminBoot>(devBoot());
  initTheme();
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <ThemeToggle title={T.toggleTheme} />
      <MessageProvider>
        <AdminApp boot={boot} />
      </MessageProvider>
    </StrictMode>,
  );
}
