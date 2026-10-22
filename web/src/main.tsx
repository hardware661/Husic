import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import './styles.css';
import './blocks.css';
import './discover.css';
import './motion.css';
import './settings.css';
import './preferences';
import './search.css';
import './collection.css';

// A previous app on this origin cached its whole shell. Updating that one
// legacy worker retires it without deleting any of the owner's downloads.
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.getRegistrations().then((registrations) => Promise.allSettled(
    registrations.filter((r) => r.active && new URL(r.active.scriptURL).pathname === '/sw.js')
      .map((r) => r.update()),
  )).catch(() => {});
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
