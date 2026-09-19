import { clsx } from 'clsx';
import { useMemo } from 'react';
import { encodeQr, qrToSvgPath } from '../lib/qr.ts';

/** QR code as inline SVG (light on dark studio surfaces is hard to scan, so the plate is white). */
export function QRCode({ value, className, size = 160, label }: { value: string; className?: string; size?: number; label?: string }) {
  const qr = useMemo(() => {
    try {
      return encodeQr(value, 'M');
    } catch {
      return null;
    }
  }, [value]);
  if (!qr) return null;
  const quiet = 2;
  const dim = qr.size + quiet * 2;
  return (
    <svg
      viewBox={`0 0 ${dim} ${dim}`}
      width={size}
      height={size}
      role="img"
      aria-label={label ?? `QR-код: ${value}`}
      className={clsx('rounded-md bg-white', className)}
      shapeRendering="crispEdges"
    >
      <path transform={`translate(${quiet} ${quiet})`} d={qrToSvgPath(qr)} fill="#0a0f2a" />
    </svg>
  );
}
