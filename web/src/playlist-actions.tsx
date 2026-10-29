import { useEffect, useRef, useState } from 'react';
import { Icon } from './icons';

/** Native light dismissal keeps this small menu out of scroll/stacking traps. */
export function PlaylistActions({ onRename, onDelete }: { onRename: () => void; onDelete: () => void }) {
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const close = (focus = false) => {
    panel.current?.hidePopover();
    if (focus) trigger.current?.focus();
  };
  const show = () => {
    const menu = panel.current,
      button = trigger.current;
    if (!menu || !button) return;
    menu.showPopover();
    const anchor = button.getBoundingClientRect();
    menu.style.left = `${Math.max(8, Math.min(anchor.right - menu.offsetWidth, innerWidth - menu.offsetWidth - 8))}px`;
    menu.style.top = `${Math.max(8, Math.min(anchor.bottom + 6, innerHeight - menu.offsetHeight - 8))}px`;
    menu.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus();
  };
  useEffect(() => {
    if (!open) return;
    const dismiss = (event: Event) => {
      if (!(event.target instanceof Node) || !panel.current?.contains(event.target)) panel.current?.hidePopover();
    };
    document.addEventListener('scroll', dismiss, true);
    window.addEventListener('resize', dismiss);
    return () => {
      document.removeEventListener('scroll', dismiss, true);
      window.removeEventListener('resize', dismiss);
    };
  }, [open]);
  return (
    <>
      <button
        ref={trigger}
        className="icon-button"
        type="button"
        aria-label="歌单操作"
        title="歌单操作"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => (panel.current?.matches(':popover-open') ? close(true) : show())}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown') {
            event.preventDefault();
            show();
          }
        }}
      >
        <Icon name="more" size={21} />
      </button>
      <div
        ref={panel}
        popover="auto"
        className="menu playlist-actions-menu"
        role="menu"
        aria-label="歌单操作"
        onToggle={(event) => setOpen(event.newState === 'open')}
        onKeyDown={(event) => {
          const items = [...(panel.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])];
          const index = items.indexOf(document.activeElement as HTMLButtonElement);
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault();
            items[(index + (event.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length]?.focus();
          }
          if (event.key === 'Home' || event.key === 'End') {
            event.preventDefault();
            items[event.key === 'Home' ? 0 : items.length - 1]?.focus();
          }
          if (event.key === 'Escape' || event.key === 'Tab') {
            event.preventDefault();
            event.stopPropagation();
            close(true);
          }
        }}
      >
        <button
          role="menuitem"
          onClick={() => {
            close(true);
            onRename();
          }}
        >
          重命名
        </button>
        <button
          role="menuitem"
          className="danger"
          onClick={() => {
            close(true);
            onDelete();
          }}
        >
          删除歌单
        </button>
      </div>
    </>
  );
}
