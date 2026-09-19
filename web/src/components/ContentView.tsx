import { clsx } from 'clsx';
import { useEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { contentUrl } from '../api/client.ts';
import type { AnswerOptionsPayload, ContentItemView, ContentPayload } from '../ws/types.ts';
import { HexButton, HexPlate } from './HexPlate.tsx';

export interface ContentViewProps {
  content: ContentPayload[];
  /** Only the latest group of this phase is shown (the engine plays groups sequentially). */
  phase?: 'question' | 'answer';
  size?: 'sm' | 'md' | 'tv';
  /** Called when an audio/video item ends (player sends MEDIA_COMPLETED). */
  onMediaEnded?: () => void;
  autoplay?: boolean;
  muted?: boolean;
  /** Pause media (CONTENT_STATE paused / game pause). */
  paused?: boolean;
  className?: string;
}

/**
 * Renders the current content group: screen items (text/image/audio/video/html)
 * and replic items (the showman's line). Background items play hidden.
 */
export function ContentView({ content, phase = 'question', size = 'md', onMediaEnded, autoplay = true, muted = false, paused = false, className }: ContentViewProps) {
  const group = useMemo(() => {
    const ofPhase = content.filter((c) => c.phase === phase);
    return ofPhase.length ? ofPhase[ofPhase.length - 1]! : null;
  }, [content, phase]);
  if (!group) return null;
  const screen = group.items.filter((i) => i.placement === 'screen');
  const replic = group.items.filter((i) => i.placement === 'replic');
  const background = group.items.filter((i) => i.placement === 'background');
  const key = `${group.phase}-${group.index}`;
  return (
    <div key={key} className={clsx('animate-fade-in flex w-full flex-col items-center gap-3', className)}>
      {replic.map((it, i) => (
        <p key={`r${i}`} className={clsx('text-center text-muted italic', size === 'tv' ? 'text-3xl' : 'text-sm')}>
          {it.text}
        </p>
      ))}
      {screen.map((it, i) => (
        <ContentItem key={`s${i}`} item={it} size={size} onEnded={onMediaEnded} autoplay={autoplay} muted={muted} paused={paused} />
      ))}
      {background.map((it, i) => (
        <ContentItem key={`b${i}`} item={it} size={size} onEnded={onMediaEnded} autoplay={autoplay} muted={muted} paused={paused} hidden />
      ))}
    </div>
  );
}

function ContentItem({
  item,
  size,
  onEnded,
  autoplay,
  muted,
  paused,
  hidden = false,
}: {
  item: ContentItemView;
  size: 'sm' | 'md' | 'tv';
  onEnded?: () => void;
  autoplay: boolean;
  muted: boolean;
  paused: boolean;
  hidden?: boolean;
}) {
  const url = contentUrl(item);
  const mediaRef = useRef<HTMLMediaElement>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const el = mediaRef.current;
    if (!el) return;
    if (paused) el.pause();
    else if (autoplay) el.play().catch(() => undefined);
  }, [paused, autoplay, url]);

  if (item.type === 'text') {
    return (
      <p
        className={clsx(
          'max-w-4xl text-center font-semibold text-balance whitespace-pre-line',
          size === 'tv' ? 'text-5xl leading-tight' : size === 'md' ? 'text-xl' : 'text-base',
        )}
      >
        {item.text}
      </p>
    );
  }
  if (!url) return <span className="text-xs text-muted">[{item.type}]</span>;
  if (failed) return <span className="text-xs text-danger">Не удалось загрузить медиа</span>;
  const maxH = size === 'tv' ? 'max-h-[62vh]' : size === 'md' ? 'max-h-[45vh]' : 'max-h-48';
  switch (item.type) {
    case 'image':
      return <img src={url} alt="" className={clsx('rounded-md object-contain shadow-lg', maxH, 'max-w-full')} onError={() => setFailed(true)} />;
    case 'video':
      return (
        <video
          ref={mediaRef as RefObject<HTMLVideoElement>}
          src={url}
          className={clsx('rounded-md shadow-lg', maxH, 'max-w-full', hidden && 'hidden')}
          autoPlay={autoplay}
          muted={muted}
          playsInline
          controls={!autoplay}
          onEnded={onEnded}
          onError={() => setFailed(true)}
        />
      );
    case 'audio':
      return (
        <div className={clsx('flex flex-col items-center gap-2', hidden && 'hidden')}>
          <AudioWave size={size} />
          <audio
            ref={mediaRef as RefObject<HTMLAudioElement>}
            src={url}
            autoPlay={autoplay}
            muted={muted}
            controls={!autoplay}
            onEnded={onEnded}
            onError={() => setFailed(true)}
          />
        </div>
      );
    case 'html':
      return (
        <iframe
          src={url}
          title="html"
          sandbox="allow-scripts"
          className={clsx('w-full rounded-md border border-border bg-white', size === 'tv' ? 'h-[60vh]' : 'h-64')}
        />
      );
    default:
      return null;
  }
}

function AudioWave({ size }: { size: 'sm' | 'md' | 'tv' }) {
  const bars = size === 'tv' ? 24 : 12;
  return (
    <div className={clsx('flex items-end gap-1', size === 'tv' ? 'h-24' : 'h-10')} aria-label="Звук">
      {Array.from({ length: bars }, (_, i) => (
        <span
          key={i}
          className="w-1.5 animate-pulse rounded-full bg-gradient-gold"
          style={{ height: `${30 + ((i * 37) % 60)}%`, animationDelay: `${(i % 5) * 120}ms` }}
        />
      ))}
    </div>
  );
}

/** Answer options (select questions) as hex plates. */
export function AnswerOptionsView({
  options,
  excluded = [],
  size = 'md',
  onPick,
  selected,
  className,
}: {
  options: AnswerOptionsPayload;
  excluded?: string[];
  size?: 'sm' | 'md' | 'tv';
  onPick?: (label: string) => void;
  selected?: string | null;
  className?: string;
}) {
  return (
    <div className={clsx('grid w-full gap-2', size === 'tv' ? 'grid-cols-2 gap-4' : 'grid-cols-1 sm:grid-cols-2', className)}>
      {options.options.map((o) => {
        const dead = excluded.includes(o.label);
        const text = o.content.map((c) => (c.type === 'text' ? c.text : `[${c.type}]`)).join(' ');
        const tone = selected === o.label ? 'gold' : dead ? 'dim' : 'default';
        const inner = (
          <span className={clsx('flex w-full items-center gap-3 text-left', size === 'tv' ? 'text-3xl' : 'text-base')}>
            {options.showLabels && <span className="font-display text-accent">{o.label}</span>}
            <span className="flex-1">{text}</span>
          </span>
        );
        return onPick ? (
          <HexButton key={o.label} tone={tone} padding="px-5 py-3" disabled={dead} onClick={() => onPick(o.label)} className="w-full">
            {inner}
          </HexButton>
        ) : (
          <HexPlate key={o.label} tone={tone} padding={size === 'tv' ? 'px-8 py-5' : 'px-5 py-3'} className="w-full">
            {inner}
          </HexPlate>
        );
      })}
    </div>
  );
}
