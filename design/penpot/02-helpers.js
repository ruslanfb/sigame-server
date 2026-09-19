// Helper library kept in `storage` for the following scripts.
const T = {
  bg:'#070B1F', bg2:'#0E1533', surface:'#151F45', surface2:'#1D2A5C', surface3:'#26367A', border:'#2F3F7A', borderStrong:'#4A5FB0',
  text:'#F4F6FF', muted:'#9DA8D6', textInverse:'#0A0F2A', primary:'#3D63FF', primary2:'#6F8BFF', accent:'#F5B700', accent2:'#FFD76A', accent3:'#B8860B',
  accentContrast:'#1A1200', danger:'#FF4D5A', success:'#2FD67B', warning:'#FFB020', info:'#4CC9F0',
  hexTop:'#1E2C66', hexBottom:'#0F1740', hexActiveTop:'#2F4BB0', hexActiveBottom:'#1A2A75', goldTop:'#FFE08A', goldBottom:'#E39B00',
};
storage.T = T;
const fonts = { oswald: penpot.fonts.findByName('Oswald'), manrope: penpot.fonts.findByName('Manrope') };
storage.fonts = fonts;
storage.vgrad = (top, bottom, opacity = 1) => ({ fillOpacity: opacity, fillColorGradient: { type: 'linear', startX: 0.5, startY: 0, endX: 0.5, endY: 1, width: 1, stops: [{ color: top, opacity: 1, offset: 0 }, { color: bottom, opacity: 1, offset: 1 }] } });
storage.radial = (inner, mid, outer) => ({ fillOpacity: 1, fillColorGradient: { type: 'radial', startX: 0.5, startY: 0, endX: 0.5, endY: 1, width: 1.2, stops: [{ color: inner, opacity: 1, offset: 0 }, { color: mid, opacity: 1, offset: 0.45 }, { color: outer, opacity: 1, offset: 1 }] } });
storage.hexPath = (x, y, w, h) => {
  // Path geometry via the SVG `d` string (PathCommand[] is also accepted, `content` is deprecated).
  const p = penpot.createPath(); const k = Math.min(h / 2, w * 0.06);
  p.d = `M ${x + k} ${y} L ${x + w - k} ${y} L ${x + w} ${y + h / 2} L ${x + w - k} ${y + h} L ${x + k} ${y + h} L ${x} ${y + h / 2} Z`;
  return p;
};
storage.text = (str, x, y, opts = {}) => {
  if (str === undefined || str === null || String(str).length === 0) return null; // createText('') returns null
  const t = penpot.createText(String(str)); t.x = x; t.y = y;
  if (opts.font === 'display' && fonts.oswald) fonts.oswald.applyToText(t, fonts.oswald.variants.find(v => v.fontWeight === String(opts.weight || 600)) || undefined);
  else if (fonts.manrope) fonts.manrope.applyToText(t, fonts.manrope.variants.find(v => v.fontWeight === String(opts.weight || 500)) || undefined);
  t.fontSize = String(opts.size || 16); t.fills = [{ fillColor: opts.color || T.text, fillOpacity: 1 }];
  if (opts.align) t.align = opts.align; if (opts.upper) t.textTransform = 'uppercase'; if (opts.letterSpacing) t.letterSpacing = String(opts.letterSpacing);
  t.growType = opts.grow || 'auto-width';
  // w only → wrap to width (auto-height); w+h → fixed box so verticalAlign='center' centers labels on plates.
  if (opts.w) { t.resize(opts.w, opts.h || 24); t.growType = opts.h ? 'fixed' : 'auto-height'; }
  if (opts.name) t.name = opts.name;
  return t;
};
storage.hexPlate = (parent, x, y, w, h, opts = {}) => {
  const variant = opts.variant || 'default'; const p = storage.hexPath(x, y, w, h); p.name = opts.name || 'hex/' + variant;
  const fill = variant === 'gold' ? storage.vgrad(T.goldTop, T.goldBottom) : variant === 'active' ? storage.vgrad(T.hexActiveTop, T.hexActiveBottom) : variant === 'dim' ? { fillColor: T.bg2, fillOpacity: 1 } : storage.vgrad(T.hexTop, T.hexBottom);
  p.fills = [fill];
  p.strokes = [{ strokeColor: variant === 'gold' ? T.accent2 : variant === 'active' ? T.primary2 : T.borderStrong, strokeOpacity: variant === 'dim' ? 0.4 : 1, strokeWidth: 2, strokeAlignment: 'inner' }];
  if (variant === 'gold') p.shadows = [{ style: 'drop-shadow', offsetX: 0, offsetY: 0, blur: 24, spread: 0, color: { color: T.accent, opacity: 0.55 } }];
  if (variant === 'active') p.shadows = [{ style: 'drop-shadow', offsetX: 0, offsetY: 0, blur: 24, spread: 0, color: { color: T.primary, opacity: 0.55 } }];
  parent.appendChild(p);
  if (opts.label !== undefined && String(opts.label).length > 0) {
    const t = storage.text(opts.label, x, y, { font: opts.font || 'display', size: opts.size || Math.round(h * 0.45), weight: opts.weight || 600, color: variant === 'gold' ? T.accentContrast : variant === 'dim' ? T.muted : T.text, align: 'center', upper: opts.upper, w, h });
    if (t) { t.name = 'label'; t.verticalAlign = 'center'; parent.appendChild(t); }
  }
  return p;
};
storage.add = (parent, shape) => { if (shape) parent.appendChild(shape); return shape; };
storage.bgBoard = (name, w, h, x = 0, y = 0) => {
  const b = penpot.createBoard(); b.name = name; b.x = x; b.y = y; b.resize(w, h);
  b.fills = [storage.radial('#1B2A6B', '#0B1233', '#05081A')]; b.clipContent = true; return b;
};
// Scripts position children in board-relative coordinates; boards themselves sit at an offset.
// Call after building a board to convert every child's (x, y) into a position relative to the board.
storage.fixBoard = (b) => { let n = 0; for (const c of [...b.children]) { if (!penpotUtils.isContainedIn(c, b)) { penpotUtils.setParentXY(c, c.x, c.y); n++; } } return n; };
return { fonts: { oswald: !!fonts.oswald, manrope: !!fonts.manrope } };
