import { useSyncExternalStore } from 'react';

// Suspend only presentation work. The Audio element, queue, Media Session and
// sleep deadline must remain independent of whether the screen is visible.
// Losing keyboard focus does not hide a window: WebView2 and desktop browsers
// remain visible when the user clicks the desktop or another window.
const listeners = new Set<() => void>();
let suspended = false;
let visible = !document.hidden;
function update() {
  const next = !document.hidden && !suspended;
  if (visible && !next) window.dispatchEvent(new Event('husic-before-presentation-suspend'));
  document.documentElement.toggleAttribute('data-page-hidden', !next);
  if (visible === next) return;
  visible = next;
  listeners.forEach((listener) => listener());
}
function visibility() { if (!document.hidden) suspended = false; update(); }
function suspend() { suspended = true; update(); }
function resume() { suspended = false; update(); }
function focus() { resume(); }
function subscribe(listener: () => void) {
  listeners.add(listener);
  if (listeners.size === 1) {
    document.addEventListener('visibilitychange', visibility);
    document.addEventListener('freeze', suspend);
    document.addEventListener('resume', resume);
    window.addEventListener('pagehide', suspend);
    window.addEventListener('pageshow', resume);
    window.addEventListener('focus', focus);
    update();
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size) return;
    document.removeEventListener('visibilitychange', visibility);
    document.removeEventListener('freeze', suspend);
    document.removeEventListener('resume', resume);
    window.removeEventListener('pagehide', suspend);
    window.removeEventListener('pageshow', resume);
    window.removeEventListener('focus', focus);
  };
}
export function usePageVisible() { return useSyncExternalStore(subscribe, () => visible); }
