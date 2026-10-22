// Line icons in the spirit of SF Symbols; filled ones for transport controls.

const strokes: Record<string, string> = {
  netease: 'M18.5 9A7.5 7.5 0 1 1 8 5.5M13 3.5v9.3a3.1 3.1 0 1 1-3.1-3.1H13m0-6.2 4.5 2.2',
  bilibili: 'm8 3 3 3m5-3-3 3M5 6.5h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-10a2 2 0 0 1 2-2zM8 11v3m8-3v3M9.5 17h5',
  youtube: 'M7 4.5h10a5 5 0 0 1 5 5v5a5 5 0 0 1-5 5H7a5 5 0 0 1-5-5v-5a5 5 0 0 1 5-5zm3 4 6 3.5-6 3.5z',
  settings: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8zM9.3 3.5h5.4l.6 2.2 2 .9 2-.7 2.6 4.6-1.5 1.5v2.1l1.5 1.4-2.6 4.6-2-.7-2 .9-.6 2.2H9.3l-.6-2.2-2-.9-2 .7-2.6-4.6 1.5-1.4V12l-1.5-1.5 2.6-4.6 2 .7 2-.9z',
  home: 'M3.5 10.5 12 3.8l8.5 6.7M5.8 8.8V20h4.4v-5.6h3.6V20h4.4V8.8',
  browse: 'M4 4h6.5v6.5H4zM13.5 4H20v6.5h-6.5zM4 13.5h6.5V20H4zM13.5 13.5H20V20h-6.5z',
  search: 'M10.6 17.2a6.6 6.6 0 1 0 0-13.2 6.6 6.6 0 0 0 0 13.2zM15.4 15.4 20.5 20.5',
  library: 'M4.5 4v16M8.8 4v16M13 5.2l4.6 14.4M13 5.2l3-.9 4.6 14.4-3 .9',
  heart: 'M12 20s-7.5-4.6-8.8-9.6C2.4 7.2 4.4 4.5 7.4 4.5c2 0 3.6 1.3 4.6 3 1-1.7 2.6-3 4.6-3 3 0 5 2.7 4.2 5.9C19.5 15.4 12 20 12 20z',
  shuffle: 'M3.5 7h3.3c2.2 0 3.6 1.2 4.7 3.2l1.5 2.8c1 1.9 2.5 3 4.7 3h2.8M17.6 13.3l2.9 2.7-2.9 2.7M3.5 17h3.3c1.2 0 2.2-.4 3-1.1M14.4 8.1c.8-.7 1.8-1.1 3-1.1h3.1M17.6 4.3 20.5 7l-2.9 2.7',
  repeat: 'M4 11.5V9.8C4 8.2 5.2 7 6.8 7H19M16.3 4.3 19 7l-2.7 2.7M20 12.5v1.7c0 1.6-1.2 2.8-2.8 2.8H5M7.7 19.7 5 17l2.7-2.7',
  queue: 'M8.5 6.5H20M8.5 12H20M8.5 17.5H20M4.2 6.5h.1M4.2 12h.1M4.2 17.5h.1',
  grip: 'M9 5h.01M15 5h.01M9 12h.01M15 12h.01M9 19h.01M15 19h.01',
  download: 'M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5',
  folder: 'M3 7V5h7l2 2h9v13H3z',
  lyrics: 'M4.5 5.5h15v10.2H11l-4.3 3.6v-3.6H4.5zM8 9.2h8M8 12.2h5',
  volume: 'M4 9.5v5h3.4L12 18.5v-13L7.4 9.5zM15.5 9.2a4 4 0 0 1 0 5.6M18 6.8a7.4 7.4 0 0 1 0 10.4',
  mute: 'M4 9.5v5h3.4L12 18.5v-13L7.4 9.5zM15.8 9.8l4.4 4.4M20.2 9.8l-4.4 4.4',
  more: 'M5.5 12h.1M12 12h.1M18.5 12h.1',
  plus: 'M12 5v14M5 12h14',
  chevron: 'm9 5.5 6.5 6.5L9 18.5',
  down: 'm5.5 9 6.5 6.5L18.5 9',
  close: 'm6 6 12 12M18 6 6 18',
  note: 'M9 18V6.2l10-2v11.6M9 9.6l10-2M9 18a2.6 2.1 0 1 1-5.2 0A2.6 2.1 0 0 1 9 18zM19 15.8a2.6 2.1 0 1 1-5.2 0 2.6 2.1 0 0 1 5.2 0z',
  sync: 'M19.5 12a7.5 7.5 0 0 1-13.4 4.6M4.5 12a7.5 7.5 0 0 1 13.4-4.6M18 3.8v3.8h-3.8M6 20.2v-3.8h3.8',
  check: 'm5.5 12.5 4.2 4.2 8.8-9.4',
  sparkle: 'M12 3.5c.6 3.9 2.6 5.9 6.5 6.5-3.9.6-5.9 2.6-6.5 6.5-.6-3.9-2.6-5.9-6.5-6.5 3.9-.6 5.9-2.6 6.5-6.5zM18.5 15.5c.3 1.6 1 2.3 2.5 2.5-1.5.2-2.2.9-2.5 2.5-.3-1.6-1-2.3-2.5-2.5 1.5-.2 2.2-.9 2.5-2.5z',
  radio: 'M12 13.2a1.2 1.2 0 1 0 0-2.4 1.2 1.2 0 0 0 0 2.4zM8.4 15.6a5 5 0 0 1 0-7.2M15.6 8.4a5 5 0 0 1 0 7.2M5.6 18.4a9 9 0 0 1 0-12.8M18.4 5.6a9 9 0 0 1 0 12.8',
  chart: 'M5 19.5V13M10 19.5V6M15 19.5v-9M20 19.5V9.5',
  clock: 'M12 20.5a8.5 8.5 0 1 0 0-17 8.5 8.5 0 0 0 0 17zM12 7.5V12l3 2',
};

const fills: Record<string, string> = {
  play: 'M7.5 4.6v14.8c0 .8.9 1.3 1.6.9l11.6-7.4c.6-.4.6-1.3 0-1.7L9.1 3.7c-.7-.4-1.6.1-1.6.9z',
  pause: 'M6.5 4.5h3.6v15H6.5zM13.9 4.5h3.6v15h-3.6z',
  previous: 'M11.4 12 20 6.3v11.4zM2.8 12l8.6-5.7v11.4z',
  next: 'M12.6 12 4 6.3v11.4zM21.2 12l-8.6-5.7v11.4z',
  heartFill:
    'M12 20.4s-7.8-4.7-9.2-10C2 6.9 4.2 4 7.4 4c2 0 3.6 1.1 4.6 2.7C13 5.1 14.6 4 16.6 4c3.2 0 5.4 2.9 4.6 6.4-1.4 5.3-9.2 10-9.2 10z',
};

export function Icon({ name, size = 20, className }: { name: string; size?: number; className?: string }) {
  const fill = fills[name];
  return (
    <svg className={className} width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      {fill ? (
        <path d={fill} fill="currentColor" />
      ) : (
        <path d={strokes[name]} fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" />
      )}
    </svg>
  );
}

export function PlayGlyph({ playing, size = 20 }: { playing: boolean; size?: number }) {
  return <span className={`play-glyph${playing ? ' is-playing' : ''}`} aria-hidden="true" style={{ width: size, height: size }}>
    <Icon name="play" size={size} className="glyph-play" />
    <Icon name="pause" size={size} className="glyph-pause" />
  </span>;
}
