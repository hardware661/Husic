import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react';
import { reduceMotion } from './preferences';

type Position = { x: number; y: number; w: number; h: number };
const position = (r: DOMRect): Position => ({ x: r.x, y: r.y, w: r.width, h: r.height });
type Flight = {
  node: HTMLElement; frame: number; p: Position; v: Position; target: Position;
  restore: () => void; finish: () => void;
};

/** A single physical artwork, shared by the small player and full player.
 * Retarget the live spring (including its velocity), so quick reversals never
 * wait for a canned animation to finish. React does not render per frame. */
export function useArtworkFlight(open: boolean, track: string, playing: boolean, art: RefObject<HTMLDivElement | null>) {
  const previous = useRef({ open, track });
  const active = useRef<Flight | null>(null);
  useLayoutEffect(() => {
    const before = previous.current;
    previous.current = { open, track };
    const small = document.querySelector<HTMLElement>('.mini-song .cover');
    const large = art.current?.querySelector<HTMLElement>('.np-art-current');
    const destination = () => {
      // The art wrapper is unscaled. Its rect includes a sheet drag, while the
      // image's --art-scale is the *final* pause/play scale, applied once here.
      // The spring's source below is already the live, transformed image rect;
      // never multiply that source by --art-scale again.
      const box = art.current!.getBoundingClientRect();
      const scale = Number.parseFloat(getComputedStyle(large!).getPropertyValue('--art-scale')) || 1;
      return { x: box.x + box.width * (1 - scale) / 2, y: box.y + box.height * (1 - scale) / 2, w: box.width * scale, h: box.height * scale };
    };
    if (before.open === open) {
      if (before.track !== track) active.current?.finish();
      else if (open && active.current && large && art.current) active.current.target = destination();
      return;
    }
    if (!small || !large || reduceMotion()) { active.current?.finish(); return; }
    const from = (open ? small : large).getBoundingClientRect();
    const to = (open ? large : small).getBoundingClientRect();
    if (!from.width || !to.width) { active.current?.finish(); return; }

    let flight = active.current;
    if (flight) {
      cancelAnimationFrame(flight.frame);
      flight.restore();
      flight.target = open ? destination() : position(to);
    } else {
      const node = (open ? small : large).cloneNode(true) as HTMLElement;
      node.classList.add('artwork-flight');
      node.setAttribute('aria-hidden', 'true');
      node.querySelectorAll('img').forEach((img) => { img.loading = 'eager'; img.style.opacity = '1'; img.style.transform = 'none'; });
      // A single compositor transform owns flight geometry, even when a cover
      // is interrupted halfway through its CSS pause/play transition.
      node.style.transition = 'none';
      document.body.append(node);
      flight = { node, frame: 0, p: position(from), v: { x: 0, y: 0, w: 0, h: 0 }, target: open ? destination() : position(to), restore: () => {}, finish: () => {} };
      active.current = flight;
    }
    const f = flight;
    const smallVisibility = small.style.visibility;
    const largeVisibility = large.style.visibility;
    small.style.visibility = large.style.visibility = 'hidden';
    f.restore = () => { small.style.visibility = smallVisibility; large.style.visibility = largeVisibility; };
    f.finish = () => {
      cancelAnimationFrame(f.frame);
      f.restore(); f.node.remove();
      if (active.current === f) active.current = null;
    };
    // The raster remains a fixed size; motion is compositor transforms only.
    const base = Math.max(from.width, to.width, f.p.w);
    f.node.style.width = f.node.style.height = `${base}px`;
    const paint = () => {
      f.node.style.transform = `translate3d(${f.p.x}px,${f.p.y}px,0) scale(${f.p.w / base},${f.p.h / base})`;
    };
    paint();
    let last = performance.now();
    const tick = (now: number) => {
      // A finger can start pulling before the opening spring has settled.
      // Follow that live sheet geometry, rather than flying toward a stale
      // pre-drag rectangle; quick close/reopen keeps this same physical node.
      if (open) {
        f.target = destination();
        if (!f.target.w || !f.target.h) { f.finish(); return; }
      }
      const elapsed = Math.min(0.032, (now - last) / 1000); last = now;
      // Small substeps keep a dropped frame from destabilizing the spring.
      let resting = true;
      for (const key of ['x', 'y', 'w', 'h'] as const) {
        for (let step = 0; step < 3; step++) {
          const dt = elapsed / 3;
          f.v[key] += ((f.target[key] - f.p[key]) * 420 - f.v[key] * 40) * dt;
          f.p[key] += f.v[key] * dt;
        }
        if (Math.abs(f.target[key] - f.p[key]) > 0.35 || Math.abs(f.v[key]) > 3) resting = false;
      }
      paint();
      if (resting || document.hidden) f.finish();
      else f.frame = requestAnimationFrame(tick);
    };
    f.frame = requestAnimationFrame(tick);
  }, [open, track, playing, art]);

  useEffect(() => {
    const reduced = matchMedia('(prefers-reduced-motion: reduce)');
    const finish = () => active.current?.finish();
    const visibility = () => { if (document.hidden) finish(); };
    reduced.addEventListener('change', finish);
    window.addEventListener('music-preferences-change', finish);
    document.addEventListener('visibilitychange', visibility);
    window.addEventListener('resize', finish);
    return () => { finish(); reduced.removeEventListener('change', finish); window.removeEventListener('music-preferences-change', finish); document.removeEventListener('visibilitychange', visibility); window.removeEventListener('resize', finish); };
  }, []);
}

/** Reveal complete editorial blocks, not every row. Nothing is hidden while
 * waiting for JavaScript, and no animation delays a click or data request. */
export function usePageMotion(main: HTMLElement | null, path: string) {
  const seen = useRef(new WeakSet<Element>());
  const input = useRef<'pointer' | 'keyboard'>('pointer');
  useEffect(() => {
    const pointer = () => { input.current = 'pointer'; };
    const keyboard = () => { input.current = 'keyboard'; };
    document.addEventListener('pointerdown', pointer, true);
    document.addEventListener('keydown', keyboard, true);
    return () => { document.removeEventListener('pointerdown', pointer, true); document.removeEventListener('keydown', keyboard, true); };
  }, []);
  useEffect(() => {
    if (!main) return;
    const animations = new Set<Animation>();
    const selector = '.crate, .section, .blind, .gazette, .arrivals, .collection-header, .chart-head, .podium, .song-list, .settings-group, .search-field';
    let pending = 0;
    const observer = new IntersectionObserver((entries) => {
      for (const entry of entries) {
        const node = entry.target as HTMLElement;
        node.classList.toggle('motion-offscreen', !entry.isIntersecting);
        if (!entry.isIntersecting || seen.current.has(node)) continue;
        seen.current.add(node);
        if (reduceMotion() || input.current === 'keyboard') continue;
        const animation = node.animate([
          { opacity: 0.75, transform: 'translate3d(0,6px,0)' },
          { opacity: 1, transform: 'translate3d(0,0,0)' },
        ], { duration: 260, easing: 'cubic-bezier(.22,1,.36,1)' });
        animations.add(animation);
        animation.finished.catch(() => {}).finally(() => animations.delete(animation));
      }
    }, { root: main, threshold: 0.03 });
    const observed = new Set<Element>();
    const scan = () => {
      pending = 0;
      for (const node of observed) if (!main.contains(node)) { observer.unobserve(node); observed.delete(node); }
      main.querySelectorAll(selector).forEach((node) => {
        if (node.matches('.song-list') && node.parentElement?.closest('.section')) return;
        if (!observed.has(node)) { observed.add(node); observer.observe(node); }
      });
    };
    const mutations = new MutationObserver(() => { if (!pending) pending = requestAnimationFrame(scan); });
    mutations.observe(main, { childList: true, subtree: true });
    scan();
    const visibility = () => main.closest('.frame')?.classList.toggle('motion-document-hidden', document.hidden);
    const reduced = matchMedia('(prefers-reduced-motion: reduce)');
    const simplify = () => { if (reduceMotion()) animations.forEach((a) => a.cancel()); };
    reduced.addEventListener('change', simplify);
    window.addEventListener('music-preferences-change', simplify);
    document.addEventListener('visibilitychange', visibility); visibility();
    return () => {
      observer.disconnect(); mutations.disconnect(); cancelAnimationFrame(pending);
      animations.forEach((a) => a.cancel());
      document.removeEventListener('visibilitychange', visibility); reduced.removeEventListener('change', simplify);
      window.removeEventListener('music-preferences-change', simplify);
    };
  }, [main, path]);
}
