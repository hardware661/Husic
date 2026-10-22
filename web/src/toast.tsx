import { useEffect, useState } from 'react';

// A single quiet status line above the player.

let show: (message: string) => void = () => {};

export function toast(message: string) {
  show(message);
}

export function Toaster() {
  const [message, setMessage] = useState('');
  useEffect(() => {
    let timer = 0;
    show = (m) => {
      setMessage(m);
      clearTimeout(timer);
      timer = window.setTimeout(() => setMessage(''), 2400);
    };
    return () => clearTimeout(timer);
  }, []);
  return (
    <div className={`toast${message ? ' is-visible' : ''}`} role="status" aria-live="polite">
      {message}
    </div>
  );
}
