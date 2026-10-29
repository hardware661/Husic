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

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
