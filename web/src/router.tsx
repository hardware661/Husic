import { useLayoutEffect, useSyncExternalStore, type MouseEvent, type ReactNode, type CSSProperties } from 'react';

// A tiny History-API router: the app has a handful of flat routes.

const listeners = new Set<() => void>();
const notify = () => listeners.forEach((l) => l());
const entry = () => ({ key: crypto.randomUUID(), depth: 0 });
let currentEntry: { key: string; depth: number } = history.state?.musicEntry || entry();
let navigation = { ...currentEntry, kind: 'initial' };
history.replaceState({ ...history.state, musicEntry: currentEntry }, '');
if ('scrollRestoration' in history) history.scrollRestoration = 'manual';
window.addEventListener('popstate', () => {
  window.dispatchEvent(new Event('music-before-navigation'));
  currentEntry = history.state?.musicEntry || entry();
  navigation = { ...currentEntry, kind: 'pop' };
  notify();
});

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function navigate(path: string, replace = false) {
  if (path === location.pathname + location.search) return;
  window.dispatchEvent(new Event('music-before-navigation'));
  currentEntry = replace ? currentEntry : { key: crypto.randomUUID(), depth: currentEntry.depth + 1 };
  navigation = { ...currentEntry, kind: replace ? 'replace' : 'push' };
  if (replace) history.replaceState({ ...history.state, musicEntry: currentEntry }, '', path);
  else history.pushState({ musicEntry: currentEntry }, '', path);
  notify();
}

export function goBack() {
  if (currentEntry.depth > 0) history.back();
  else navigate('/');
}

export function usePath() {
  return useSyncExternalStore(subscribe, () => location.pathname + location.search);
}

/** Matches "/playlist/:id" style patterns against the current path. */
export function match(pattern: string, path: string): Record<string, string> | null {
  const [pathname] = path.split('?');
  const want = pattern.split('/');
  const have = pathname.split('/');
  if (want.length !== have.length) return null;
  const params: Record<string, string> = {};
  for (let i = 0; i < want.length; i++) {
    if (want[i].startsWith(':')) params[want[i].slice(1)] = decodeURIComponent(have[i]);
    else if (want[i] !== have[i]) return null;
  }
  return params;
}

export function query(path: string, key: string) {
  return new URLSearchParams(path.split('?')[1] ?? '').get(key) ?? '';
}

export function Link({ to, className, children, label, style, onIntent }: { to: string; className?: string; children: ReactNode; label?: string; style?: CSSProperties; onIntent?: () => void }) {
  const onClick = (e: MouseEvent<HTMLAnchorElement>) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    navigate(to);
  };
  return (
    <a href={to} className={className} onClick={onClick} onPointerEnter={onIntent} onFocus={onIntent} onPointerDown={onIntent} aria-label={label} style={style}>
      {children}
    </a>
  );
}

/** Restores the scroll position of the main view per path. */
export function useScrollMemory(element: HTMLElement | null, path: string) {
  const visit = useSyncExternalStore(subscribe, () => navigation);
  useLayoutEffect(() => {
    if (!element) return;
    const key = `music-scroll:${visit.key}`;
    let target = 0;
    try { if (visit.kind === 'pop' || visit.kind === 'initial') target = Number(sessionStorage.getItem(key)) || 0; } catch { /* Private browsing can deny storage. */ }
    let restoring = target > 0;
    let observed: Element | null = null;
    const resize = new ResizeObserver(() => restore());
    const mutations = new MutationObserver(() => { watch(); restore(); });
    const finish = () => { restoring = false; resize.disconnect(); mutations.disconnect(); };
    const restore = () => {
      if (!restoring) return;
      element.scrollTop = target;
      if (Math.abs(element.scrollTop - target) < 2) finish();
    };
    const watch = () => {
      const page = element.firstElementChild;
      if (page !== observed) { if (observed) resize.unobserve(observed); observed = page; if (page) resize.observe(page); }
    };
    element.scrollTop = target;
    if (restoring) { mutations.observe(element, { childList: true, subtree: true }); watch(); restore(); }
    const abort = () => finish();
    const save = () => { if (!restoring) try { sessionStorage.setItem(key, String(element.scrollTop)); } catch { /* No persistence still leaves navigation usable. */ } };
    const timer = setTimeout(finish, 8000);
    window.addEventListener('music-before-navigation', save);
    element.addEventListener('scroll', save, { passive: true });
    element.addEventListener('wheel', abort, { passive: true });
    element.addEventListener('touchstart', abort, { passive: true });
    element.addEventListener('keydown', abort);
    return () => { finish(); clearTimeout(timer); window.removeEventListener('music-before-navigation', save); element.removeEventListener('scroll', save); element.removeEventListener('wheel', abort); element.removeEventListener('touchstart', abort); element.removeEventListener('keydown', abort); };
  }, [element, path, visit]);
}
