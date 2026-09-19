/**
 * Short Web Audio beep for the light. The AudioContext is created lazily on a
 * user gesture (browsers block autoplay); `prime()` should be called from a
 * pointer/key handler once, `beep()` at the light.
 */
let ctx: AudioContext | null = null;

export function primeAudio(): void {
  if (typeof window === 'undefined' || !('AudioContext' in window)) return;
  try {
    ctx ??= new AudioContext();
    if (ctx.state === 'suspended') void ctx.resume();
  } catch {
    ctx = null;
  }
}

export function beep(frequency = 880, durationMs = 70, volume = 0.15): void {
  if (!ctx || ctx.state !== 'running') return;
  try {
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = 'sine';
    osc.frequency.value = frequency;
    gain.gain.value = volume;
    osc.connect(gain).connect(ctx.destination);
    const t = ctx.currentTime;
    osc.start(t);
    gain.gain.setValueAtTime(volume, t);
    gain.gain.exponentialRampToValueAtTime(0.0001, t + durationMs / 1000);
    osc.stop(t + durationMs / 1000);
  } catch {
    /* audio is best-effort */
  }
}
