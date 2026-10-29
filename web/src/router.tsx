import { useEffect, useLayoutEffect, useSyncExternalStore, type MouseEvent, type ReactNode, type CSSProperties } from 'react';

// A tiny History-API router: the app has a handful of flat routes.

const listeners = new Set<() => void>();
const notify = () => listeners.forEach((l) => l());
const entry = () => ({ key: crypto.randomUUID(), depth: 0 });
let currentEntry: { key: string; depth: number } = history.state?.musicEntry || entry();
let navigation = { ...currentEntry, kind: 'initial' };
history.replaceState({ ...history.state, musicEntry: currentEntry }, '');
if ('scrollRestoration' in history) history.scrollRestoration = 'manual';
window.addEventListener('popstate', () => {
  window.dispatchEvent(new Event('husic-before-navigation'));
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
  window.dispatchEvent(new Event('husic-before-navigation'));
  // Phone tabs are peers. Desktop links keep normal browser/Alt+Left history.
  const rootPath = path.split('?')[0];
  const root = matchMedia('(max-width: 760px)').matches && ['/', '/discover', '/charts', '/library', '/settings'].includes(rootPath);
  replace ||= root;
  currentEntry = root
    ? { key: `tab:${rootPath}`, depth: 0 }
    : replace
      ? currentEntry
      : { key: crypto.randomUUID(), depth: currentEntry.depth + 1 };
  navigation = { ...currentEntry, kind: root && location.pathname !== rootPath ? 'tab' : replace ? 'replace' : 'push' };
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

/** Safari starts history swipes on touchstart, before a pointermove can
 * cancel them. Reserve this narrow strip for the player's own back gesture;
 * ordinary pages never swipe through browser history. Vertical scrolling
 * outside the strip and explicit Back buttons remain native. */
export function useMobileEdgeGuard() {
  useEffect(() => {
    const start = (event: TouchEvent) => {
      if (matchMedia('(max-width: 760px)').matches && event.touches.length === 1 && event.touches[0].clientX <= 24 && event.cancelable)
        event.preventDefault();
    };
    // Cancelling the start reserves the whole gesture. A non-passive global
    // touchmove would unnecessarily put every ordinary scroll on the main thread.
    document.addEventListener('touchstart', start, { passive: false });
    return () => document.removeEventListener('touchstart', start);
  }, []);
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

export function Link({
  to,
  className,
  children,
  label,
  style,
  onIntent,
}: {
  to: string;
  className?: string;
  children: ReactNode;
  label?: string;
  style?: CSSProperties;
  onIntent?: () => void;
}) {
  const onClick = (e: MouseEvent<HTMLAnchorElement>) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    navigate(to);
  };
  return (
    <a
      href={to}
      draggable={false}
      onDragStart={(event) => event.preventDefault()}
      className={className}
      onClick={onClick}
      onPointerEnter={onIntent}
      onFocus={onIntent}
      onPointerDown={onIntent}
      aria-label={label}
      style={style}
    >
      {children}
    </a>
  );
}

/** Restores the scroll position of the main view per path. */
export function useScrollMemory(element: HTMLElement | null, path: string) {
  const visit = useSyncExternalStore(subscribe, () => navigation);
  useLayoutEffect(() => {
    if (!element) return;
    const key = `husic-scroll:${visit.key}`;
    let target = 0;
    try {
      if (visit.kind === 'pop' || visit.kind === 'initial' || visit.kind === 'tab') target = Number(sessionStorage.getItem(key)) || 0;
    } catch {
      /* Private browsing can deny storage. */
    }
    let restoring = target > 0;
    let observed: Element | null = null;
    const resize = new ResizeObserver(() => restore());
    const mutations = new MutationObserver(() => {
      watch();
      restore();
    });
    const finish = () => {
      restoring = false;
      resize.disconnect();
      mutations.disconnect();
    };
    const restore = () => {
      if (!restoring) return;
      element.scrollTop = target;
      if (Math.abs(element.scrollTop - target) < 2) finish();
    };
    const watch = () => {
      const page = element.querySelector('[data-route-active="true"]') ?? element.firstElementChild;
      if (page !== observed) {
        if (observed) resize.unobserve(observed);
        observed = page;
        if (page) resize.observe(page);
      }
    };
    element.scrollTop = target;
    if (restoring) {
      mutations.observe(element, { childList: true, subtree: true });
      watch();
      restore();
    }
    const abort = () => finish();
    let saveTimer: ReturnType<typeof setTimeout> | undefined;
    const save = () => {
      clearTimeout(saveTimer);
      saveTimer = undefined;
      if (!restoring)
        try {
          sessionStorage.setItem(key, String(element.scrollTop));
        } catch {
          /* No persistence still leaves navigation usable. */
        }
    };
    const scheduleSave = () => {
      clearTimeout(saveTimer);
      saveTimer = setTimeout(save, 180);
    };
    const timer = setTimeout(finish, 8000);
    window.addEventListener('husic-before-navigation', save);
    window.addEventListener('husic-before-presentation-suspend', save);
    window.addEventListener('pagehide', save);
    element.addEventListener('scroll', scheduleSave, { passive: true });
    element.addEventListener('wheel', abort, { passive: true });
    element.addEventListener('touchstart', abort, { passive: true });
    element.addEventListener('keydown', abort);
    return () => {
      clearTimeout(saveTimer);
      finish();
      clearTimeout(timer);
      window.removeEventListener('husic-before-navigation', save);
      window.removeEventListener('husic-before-presentation-suspend', save);
      window.removeEventListener('pagehide', save);
      element.removeEventListener('scroll', scheduleSave);
      element.removeEventListener('wheel', abort);
      element.removeEventListener('touchstart', abort);
      element.removeEventListener('keydown', abort);
    };
  }, [element, path, visit]);
}
