import { useEffect, useState, useSyncExternalStore } from 'react';
import { usePageVisible } from './view-activity';

const key = 'husic-sleep-timer-v1';
const listeners = new Set<() => void>();
let deadline: number | null = null;
let duration = 30 * 60;
try {
  const saved = JSON.parse(localStorage.getItem(key) || '{}');
  if (Number.isSafeInteger(saved.duration) && saved.duration > 0 && saved.duration < 86400) duration = saved.duration;
  if (Number.isSafeInteger(saved.deadline) && saved.deadline > 0) deadline = saved.deadline;
} catch {
  /* A timer still works when this browser cannot save preferences. */
}

function publish() {
  try {
    localStorage.setItem(key, JSON.stringify({ deadline, duration }));
  } catch {
    /* Session-only is fine. */
  }
  listeners.forEach((listener) => listener());
}
export function startSleepTimer(seconds: number) {
  if (!Number.isSafeInteger(seconds) || seconds < 1 || seconds >= 86400) return;
  duration = seconds;
  deadline = Date.now() + seconds * 1000;
  publish();
}
export function cancelSleepTimer() {
  deadline = null;
  publish();
}
export function lastSleepDuration() {
  return duration;
}
export function consumeExpiredSleepTimer() {
  if (deadline === null || Date.now() < deadline) return false;
  deadline = null;
  publish();
  return true;
}
function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
export function useSleepDeadline() {
  return useSyncExternalStore(subscribe, () => deadline);
}
export function useSleepRemaining() {
  const end = useSleepDeadline();
  const visible = usePageVisible();
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!visible) return;
    setNow(Date.now());
    if (end === null) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [end, visible]);
  return { deadline: end, seconds: end === null ? 0 : Math.max(0, Math.ceil((end - now) / 1000)) };
}
export function sleepClock(seconds: number) {
  return [Math.floor(seconds / 3600), Math.floor(seconds / 60) % 60, seconds % 60].map((value) => String(value).padStart(2, '0')).join(':');
}

/** One deadline for this device, independent of the settings page and track.
 * Audio events also reconcile it when a mobile browser throttles JS timers. */
export function useSleepTimerPlayback(audio: HTMLAudioElement, stopIfExpired: () => boolean) {
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    let watchingTime = false;
    const check = () => {
      clearTimeout(timer);
      const expired = stopIfExpired(),
        active = deadline !== null;
      if (watchingTime !== active) {
        watchingTime = active;
        if (active) audio.addEventListener('timeupdate', check);
        else audio.removeEventListener('timeupdate', check);
      }
      if (expired || deadline === null) return;
      timer = setTimeout(check, Math.max(1, Math.min(60000, deadline - Date.now())));
    };
    const storage = (event: StorageEvent) => {
      if (event.key !== key) return;
      try {
        const saved = JSON.parse(event.newValue || '{}');
        deadline = Number.isSafeInteger(saved.deadline) && saved.deadline > 0 ? saved.deadline : null;
      } catch {
        deadline = null;
      }
      listeners.forEach((listener) => listener());
    };
    const unsubscribe = subscribe(check);
    const events = ['playing', 'ended', 'loadedmetadata'];
    events.forEach((event) => audio.addEventListener(event, check));
    document.addEventListener('visibilitychange', check);
    window.addEventListener('pageshow', check);
    window.addEventListener('focus', check);
    window.addEventListener('storage', storage);
    check();
    return () => {
      clearTimeout(timer);
      unsubscribe();
      audio.removeEventListener('timeupdate', check);
      events.forEach((event) => audio.removeEventListener(event, check));
      document.removeEventListener('visibilitychange', check);
      window.removeEventListener('pageshow', check);
      window.removeEventListener('focus', check);
      window.removeEventListener('storage', storage);
    };
  }, [audio, stopIfExpired]);
}
