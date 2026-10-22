import { useEffect, useId, useRef, useState } from 'react';
import { Icon } from './icons';

/** Small, local forms; the browser handles modal focus and background inertness. */
export function ActionDialog({ title, description, inputLabel, confirmLabel, onConfirm, onClose }: {
  title: string; description?: string; inputLabel?: string; confirmLabel: string;
  onConfirm: (value: string) => Promise<void>; onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const heading = useId();
  const field = useId();
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    const previous = document.activeElement;
    const node = ref.current;
    node?.showModal();
    return () => { node?.close(); if (previous instanceof HTMLElement && previous.isConnected) previous.focus({ preventScroll: true }); };
  }, []);
  return <dialog className="action-dialog" ref={ref} aria-labelledby={heading}
    onKeyDown={(event) => event.stopPropagation()}
    onCancel={(event) => { event.preventDefault(); if (!busy) onClose(); }}>
    <form onSubmit={async (event) => {
      event.preventDefault();
      if (busy || inputLabel && !value.trim()) return;
      setBusy(true); setError('');
      try { await onConfirm(value.trim()); onClose(); }
      catch { setError('这次没有保存成功，请稍后再试。'); setBusy(false); }
    }}>
      <header><h2 id={heading}>{title}</h2><button type="button" className="icon-button" aria-label="关闭" disabled={busy} onClick={onClose}><Icon name="close" /></button></header>
      {description && <p className="action-dialog-description">{description}</p>}
      {inputLabel && <label className="action-dialog-field" htmlFor={field}>{inputLabel}<input id={field} autoFocus maxLength={80} value={value} onChange={(event) => setValue(event.target.value)} disabled={busy} autoComplete="off" required /></label>}
      {error && <p className="action-dialog-error" role="alert">{error}</p>}
      <footer><button type="button" className="pill" onClick={onClose} disabled={busy}>取消</button><button type="submit" className="pill primary" disabled={busy || Boolean(inputLabel && !value.trim())}>{busy ? '正在保存…' : confirmLabel}</button></footer>
    </form>
  </dialog>;
}
