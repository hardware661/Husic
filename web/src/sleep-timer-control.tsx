import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Icon } from './icons';
import { reduceMotion } from './preferences';
import { cancelSleepTimer, lastSleepDuration, sleepClock, startSleepTimer, useSleepRemaining } from './sleep-timer';

const rowHeight = 44;

function TimeWheel({ label, max, value, onChange }: { label: string; max: number; value: number; onChange: (value: number) => void }) {
  const scroll = useRef<HTMLDivElement>(null);
  const emitted = useRef(-1);
  useLayoutEffect(() => {
    if (!scroll.current || emitted.current === value) return;
    const position = () => {
      if (scroll.current) {
        scroll.current.scrollTop = value * rowHeight;
        emitted.current = value;
      }
    };
    if (scroll.current.getClientRects().length) position();
    else {
      const frame = requestAnimationFrame(position);
      return () => cancelAnimationFrame(frame);
    }
  }, [value]);
  const select = (next: number, smooth = false) => {
    next = Math.max(0, Math.min(max, next));
    emitted.current = next;
    onChange(next);
    scroll.current?.scrollTo({ top: next * rowHeight, behavior: smooth && !reduceMotion() ? 'smooth' : 'instant' });
  };
  const keyDown = (event: KeyboardEvent) => {
    const next = (
      { ArrowUp: value - 1, ArrowDown: value + 1, PageUp: value - 5, PageDown: value + 5, Home: 0, End: max } as Record<string, number>
    )[event.key];
    if (next === undefined) return;
    event.preventDefault();
    select(next);
  };
  return (
    <div
      className="timer-wheel"
      role="spinbutton"
      tabIndex={0}
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={value}
      aria-valuetext={value + ' ' + label}
      onKeyDown={keyDown}
    >
      <div
        className="timer-wheel-scroll"
        ref={scroll}
        onScroll={(event) => {
          const next = Math.max(0, Math.min(max, Math.round(event.currentTarget.scrollTop / rowHeight)));
          if (next !== emitted.current) {
            emitted.current = next;
            onChange(next);
          }
        }}
        aria-hidden="true"
      >
        {Array.from({ length: max + 1 }, (_, index) => (
          <button
            key={index}
            type="button"
            tabIndex={-1}
            className={index === value ? 'is-selected' : ''}
            onClick={() => select(index, true)}
          >
            {String(index).padStart(2, '0')}
          </button>
        ))}
      </div>
      <span className="timer-wheel-unit" aria-hidden="true">
        {label}
      </span>
    </div>
  );
}

export function SleepTimerControl() {
  const { deadline, seconds } = useSleepRemaining();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState(lastSleepDuration);
  const dialog = useRef<HTMLDialogElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) return;
    const node = dialog.current;
    node?.showModal();
    return () => {
      node?.close();
      if (!document.documentElement.hasAttribute('data-page-hidden')) trigger.current?.focus({ preventScroll: true });
    };
  }, [open]);
  const hours = Math.floor(selected / 3600),
    minutes = Math.floor(selected / 60) % 60,
    secs = selected % 60;
  const close = () => setOpen(false);
  return (
    <>
      <button
        className={'sleep-timer-setting' + (deadline ? ' is-active' : '')}
        type="button"
        aria-label="定时停止播放"
        ref={trigger}
        onClick={() => {
          setSelected(lastSleepDuration());
          setOpen(true);
        }}
      >
        <span className="setting-symbol">
          <Icon name="clock" />
        </span>
        <span className="setting-copy">
          <strong>定时停止播放</strong>
        </span>
        <span className="sleep-timer-summary">
          {deadline ? sleepClock(seconds) : '未开启'}
          <Icon name="chevron" size={16} />
        </span>
      </button>
      {open && (
        <dialog
          className="sleep-timer-dialog"
          aria-label="定时停止播放"
          ref={dialog}
          onCancel={(event) => {
            event.preventDefault();
            close();
          }}
          onClick={(event) => {
            if (event.target === event.currentTarget) {
              const r = event.currentTarget.getBoundingClientRect();
              if (event.clientX < r.left || event.clientX > r.right || event.clientY < r.top || event.clientY > r.bottom) close();
            }
          }}
        >
          <header>
            <h2>定时停止播放</h2>
            <button type="button" className="icon-button" aria-label="关闭定时设置" onClick={close}>
              <Icon name="close" size={20} />
            </button>
          </header>
          {deadline && (
            <div className="sleep-timer-countdown">
              <output aria-label="剩余时间">{sleepClock(seconds)}</output>
              <span>后停止播放</span>
            </div>
          )}
          <div className="timer-wheels" role="group" aria-label="计时时长">
            <TimeWheel label="小时" max={23} value={hours} onChange={(value) => setSelected(value * 3600 + minutes * 60 + secs)} />
            <TimeWheel label="分钟" max={59} value={minutes} onChange={(value) => setSelected(hours * 3600 + value * 60 + secs)} />
            <TimeWheel label="秒" max={59} value={secs} onChange={(value) => setSelected(hours * 3600 + minutes * 60 + value)} />
          </div>
          <div className="sleep-timer-actions">
            {deadline && (
              <button
                className="pill"
                type="button"
                onClick={() => {
                  cancelSleepTimer();
                  close();
                }}
              >
                取消定时
              </button>
            )}
            <button
              className="pill primary"
              type="button"
              disabled={!selected}
              onClick={() => {
                startSleepTimer(selected);
                close();
              }}
            >
              {deadline ? '重新计时' : '开始计时'}
            </button>
          </div>
        </dialog>
      )}
    </>
  );
}
