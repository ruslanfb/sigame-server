// Helper library v2 (flex-based), kept in `storage` for the following scripts.
// Every composite element is a board with a flex layout, so centering/spacing is done by Penpot, not by hand-computed
// coordinates. Texts are always FIXED boxes (explicit w/h, align + verticalAlign) because plugin-created texts are
// measured asynchronously; a fixed box renders in the right place even before the measurement lands.
const T = {
  bg:'#070B1F', bg2:'#0E1533', surface:'#151F45', surface2:'#1D2A5C', surface3:'#26367A', border:'#2F3F7A', borderStrong:'#4A5FB0',
  text:'#F4F6FF', muted:'#9DA8D6', textInverse:'#0A0F2A', primary:'#3D63FF', primary2:'#6F8BFF', accent:'#F5B700', accent2:'#FFD76A', accent3:'#B8860B',
  accentContrast:'#1A1200', danger:'#FF4D5A', success:'#2FD67B', warning:'#FFB020', info:'#4CC9F0',
  hexTop:'#26377F', hexBottom:'#131D4D', hexActiveTop:'#3A5AD0', hexActiveBottom:'#1E3090', goldTop:'#FFE08A', goldBottom:'#E39B00',
};
storage.T = T;
const fonts = { oswald: penpot.fonts.findByName('Oswald'), manrope: penpot.fonts.findByName('Manrope') };
storage.fonts = fonts;
storage.sleep = (ms) => new Promise(r => setTimeout(r, ms));
storage.solid = (c, a = 1) => ({ fillColor: c, fillOpacity: a });
storage.line = (c, w = 1, a = 1) => ({ strokeColor: c, strokeOpacity: a, strokeWidth: w, strokeAlignment: 'inner' });
storage.glow = (c, blur = 24, a = 0.55) => ({ style: 'drop-shadow', offsetX: 0, offsetY: 0, blur, spread: 0, color: { color: c, opacity: a } });
storage.vgrad = (top, bottom, opacity = 1) => ({ fillOpacity: opacity, fillColorGradient: { type: 'linear', startX: 0.5, startY: 0, endX: 0.5, endY: 1, width: 1, stops: [{ color: top, opacity: 1, offset: 0 }, { color: bottom, opacity: 1, offset: 1 }] } });
storage.radial = (inner, mid, outer) => ({ fillOpacity: 1, fillColorGradient: { type: 'radial', startX: 0.5, startY: 0, endX: 0.5, endY: 1, width: 1.2, stops: [{ color: inner, opacity: 1, offset: 0 }, { color: mid, opacity: 1, offset: 0.45 }, { color: outer, opacity: 1, offset: 1 }] } });
// Hexagon path in local coordinates (0,0)-(w,h); the "cut" k grows with width but never exceeds half the height.
storage.hexD = (w, h) => { const k = Math.min(h / 2, Math.max(10, Math.round(w * 0.06))); return `M ${k} 0 L ${w - k} 0 L ${w} ${h / 2} L ${w - k} ${h} L ${k} ${h} L 0 ${h / 2} Z`; };

// text(str, o): o = { font: 'sans'|'display', size, weight, color, align, valign, upper, letterSpacing, w, h, lines, name }
storage.text = (str, o = {}) => {
  if (str === undefined || str === null || String(str).length === 0) return null; // createText('') returns null
  const s = String(str); const t = penpot.createText(s); const size = o.size || 16;
  if (o.font === 'display' && fonts.oswald) fonts.oswald.applyToText(t, fonts.oswald.variants.find(v => v.fontWeight === String(o.weight || 600)) || undefined);
  else if (fonts.manrope) fonts.manrope.applyToText(t, fonts.manrope.variants.find(v => v.fontWeight === String(o.weight || 500)) || undefined);
  t.fontSize = String(size); t.fills = [storage.solid(o.color || T.text)];
  t.align = o.align || 'left'; if (o.upper) t.textTransform = 'uppercase'; if (o.letterSpacing) t.letterSpacing = String(o.letterSpacing);
  const lines = o.lines || 1;
  const w = o.w || Math.ceil(s.length * size * (o.font === 'display' ? 0.5 : 0.62) + (o.letterSpacing || 0) * s.length + 8);
  const h = o.h || Math.ceil(size * 1.3 * lines);
  t.resize(w, h); t.growType = 'fixed'; t.verticalAlign = o.valign || (lines > 1 ? 'top' : 'center');
  t.name = o.name || 'text';
  return t;
};
// flex(board, dir, o): o = { justify, align, gap|rowGap|colGap, p|px|py|pt|pr|pb|pl, hs, vs, wrap }
storage.flex = (b, dir, o = {}) => {
  const f = b.addFlexLayout(); f.dir = dir;
  f.justifyContent = o.justify || 'start'; f.alignItems = o.align || 'center';
  const gap = o.gap ?? 0; f.rowGap = o.rowGap ?? gap; f.columnGap = o.colGap ?? gap;
  f.topPadding = o.pt ?? o.py ?? o.p ?? 0; f.bottomPadding = o.pb ?? o.py ?? o.p ?? 0;
  f.leftPadding = o.pl ?? o.px ?? o.p ?? 0; f.rightPadding = o.pr ?? o.px ?? o.p ?? 0;
  if (o.hs) f.horizontalSizing = o.hs; if (o.vs) f.verticalSizing = o.vs; if (o.wrap) f.wrap = o.wrap;
  return f;
};
storage.box = (name, w, h, o = {}) => { const b = penpot.createBoard(); b.name = name; b.resize(w, h); b.fills = o.fill ? [o.fill] : []; b.clipContent = !!o.clip; if (o.radius) b.borderRadius = o.radius; if (o.stroke) b.strokes = [o.stroke]; if (o.shadow) b.shadows = [o.shadow]; return b; };
storage.col = (name, w, h, o = {}) => { const b = storage.box(name, w, h, o); storage.flex(b, 'column', o); return b; };
storage.row = (name, w, h, o = {}) => { const b = storage.box(name, w, h, o); storage.flex(b, 'row', o); return b; };
// add(parent, child, lo): append (through the flex layout when present) and apply child layout options.
// lo = { fill (horizontal), fillH (vertical), alignSelf, z, mt|mr|mb|ml, abs: {x, y} (absolute, board-relative) }
storage.add = (parent, child, lo = {}) => {
  if (!child) return null;
  if (parent.flex) parent.flex.appendChild(child); else parent.appendChild(child);
  const lc = child.layoutChild;
  if (lc) {
    if (lo.fill) lc.horizontalSizing = 'fill'; if (lo.fillH) lc.verticalSizing = 'fill';
    if (lo.alignSelf) lc.alignSelf = lo.alignSelf; if (lo.z !== undefined) lc.zIndex = lo.z;
    if (lo.mt) lc.topMargin = lo.mt; if (lo.mr) lc.rightMargin = lo.mr; if (lo.mb) lc.bottomMargin = lo.mb; if (lo.ml) lc.leftMargin = lo.ml;
    if (lo.abs) { lc.absolute = true; penpotUtils.setParentXY(child, lo.abs.x || 0, lo.abs.y || 0); }
  }
  return child;
};
storage.many = (parent, children, lo) => children.forEach(c => storage.add(parent, c, lo));
storage.rect = (name, w, h, o = {}) => { const r = penpot.createRectangle(); r.name = name; r.resize(w, h); if (o.radius) r.borderRadius = o.radius; r.fills = [o.fill || storage.solid(T.surface)]; if (o.stroke) r.strokes = [o.stroke]; if (o.shadow) r.shadows = [o.shadow]; return r; };
storage.dot = (d, color, name = 'dot') => { const e = penpot.createEllipse(); e.name = name; e.resize(d, d); e.fills = [storage.solid(color)]; return e; };
storage.spacer = (w = 1, h = 1) => storage.box('spacer', w, h);

// Hex plate variants: fill, stroke colour, stroke width, glow, stroke opacity.
const V = {
  default: [storage.vgrad(T.hexTop, T.hexBottom), T.borderStrong, 2, null, 1],
  active:  [storage.vgrad(T.hexActiveTop, T.hexActiveBottom), T.primary2, 2, storage.glow(T.primary), 1],
  gold:    [storage.vgrad(T.goldTop, T.goldBottom), T.accent2, 2, storage.glow(T.accent), 1],
  dim:     [storage.solid(T.bg2), T.borderStrong, 2, null, 0.4],
  lit:     [storage.vgrad('#FFFFFF', T.accent2), '#FFFFFF', 4, storage.glow(T.accent, 60, 0.7), 1],
  armed:   [storage.vgrad(T.accent2, T.accent), T.accent2, 3, storage.glow(T.accent, 40, 0.6), 1],
  idle:    [storage.solid('#2A3566'), T.borderStrong, 3, null, 1],
  pressed: [storage.vgrad(T.primary2, T.primary), T.primary2, 3, storage.glow(T.primary, 40, 0.6), 1],
  locked:  [storage.vgrad('#FF8A93', T.danger), '#FF8A93', 3, storage.glow(T.danger, 40, 0.6), 1],
  success: [storage.vgrad('#3BE08A', '#1FA85E'), '#8CF5BF', 3, storage.glow(T.success, 48, 0.6), 1],
  danger:  [storage.vgrad('#FF7A84', '#C62B38'), '#FFB3B9', 3, storage.glow(T.danger, 40, 0.6), 1],
};
storage.variants = V;
// plate(w, h, o): a flex row board (centered) + absolute hex background + fixed label box centered both ways.
// o = { variant, label, font, size, weight, color, upper, name }
storage.plate = (w, h, o = {}) => {
  const variant = o.variant || 'default';
  const px = Math.round(w * 0.06) + 6;
  const b = storage.row(o.name || 'plate/' + variant, w, h, { justify: 'center', align: 'center', px });
  const p = penpot.createPath(); p.name = 'bg'; p.d = storage.hexD(w, h);
  const [fill, stroke, sw, shadow, sa] = V[variant] || V.default;
  p.fills = [fill]; p.strokes = [storage.line(stroke, sw, sa)]; if (shadow) p.shadows = [shadow];
  storage.add(b, p, { abs: { x: 0, y: 0 }, z: 0 });
  const dark = variant === 'gold' || variant === 'lit' || variant === 'armed';
  const t = storage.text(o.label, { font: o.font || 'display', size: o.size || Math.round(h * 0.45), weight: o.weight || (o.font === 'sans' ? 700 : 600),
    color: o.color || (dark ? T.accentContrast : variant === 'dim' ? T.muted : T.text), align: 'center', valign: 'center', upper: o.upper, w: w - 2 * px, h, name: 'label' });
  if (t) storage.add(b, t, { z: 1 });
  return b;
};
storage.btn = (label, w, h = 44, variant = 'default', o = {}) => storage.plate(w, h, { variant, label, font: 'sans', size: o.size || (h >= 56 ? 20 : 15), weight: 700, upper: o.upper, name: 'btn/' + label });
// Progress / timer bar: rounded track + absolute gradient fill.
storage.bar = (w, h, frac, top = T.accent2, bottom = T.accent, name = 'timer') => {
  const b = storage.row(name, w, h, {}); b.fills = [storage.solid(T.bg2)]; b.borderRadius = h / 2; b.clipContent = true;
  storage.add(b, storage.rect('fill', Math.max(h, Math.round(w * frac)), h, { radius: h / 2, fill: storage.vgrad(top, bottom), shadow: storage.glow(bottom, 16, 0.6) }), { abs: { x: 0, y: 0 } });
  return b;
};
// Card / panel: rounded surface with an optional uppercase title.
storage.card = (name, w, h, o = {}) => {
  const c = storage.col(name, w, h, { p: o.p ?? 20, gap: o.gap ?? 12, align: o.align || 'start', justify: o.justify, fill: o.fill || storage.solid(T.surface, o.alpha ?? 0.9), stroke: storage.line(o.strokeColor || T.border, o.strokeWidth || 1), radius: o.radius ?? 16, clip: true, vs: o.vs, hs: o.hs });
  if (o.title) storage.add(c, storage.text(o.title, { size: 12, weight: 700, color: T.muted, upper: true, letterSpacing: 2, w: w - 2 * (o.p ?? 20), name: 'title' }));
  return c;
};
// Input field: dark rounded box with value or placeholder.
storage.input = (w, h, value, placeholder, o = {}) => {
  const r = storage.row('input', w, h, { px: 16, align: 'center', fill: storage.solid(T.bg2), stroke: storage.line(o.focus ? T.primary2 : T.border, 1), radius: o.radius ?? 12 });
  storage.add(r, storage.text(value || placeholder, { size: o.size || 16, color: value ? T.text : T.muted, w: w - 32 }));
  return r;
};
// Labelled field (label above the input).
storage.field = (w, label, value, o = {}) => { const c = storage.col('field/' + label, w, 20 + 8 + (o.h || 44), { gap: 8, align: 'start' }); storage.add(c, storage.text(label, { size: 12, color: T.muted, w })); storage.add(c, storage.input(w, o.h || 44, value, o.placeholder || '', { size: 15, radius: 10 })); return c; };
// Person row: status dot, name, status text on the right.
storage.person = (w, h, name, status, ok, o = {}) => {
  const r = storage.row('person/' + name, w, h, { px: 16, gap: 12, align: 'center', fill: storage.solid(o.me ? T.surface3 : T.surface), stroke: storage.line(o.me ? T.accent : T.border, o.me ? 2 : 1), radius: 12 });
  storage.add(r, storage.dot(12, ok ? T.success : T.muted));
  storage.add(r, storage.text(name, { size: o.size || 16, weight: 600, w: 10 }), { fill: true });
  storage.add(r, storage.text(status, { size: 13, color: ok ? T.success : T.muted, align: 'right', w: o.statusW || 120 }));
  return r;
};
// Score card (name + score), used in strips and side panels.
storage.score = (w, h, name, score, me, o = {}) => {
  const neg = String(score).startsWith('-') || String(score).startsWith('−');
  const c = storage.col('score/' + name, w, h, { p: 10, gap: 2, align: 'start', justify: 'center', fill: storage.solid(me ? T.surface3 : T.surface), stroke: storage.line(me ? T.accent : T.border, me ? 2 : 1), radius: 12 });
  storage.add(c, storage.text(name, { size: o.nameSize || 13, color: T.muted, w: w - 20 }));
  storage.add(c, storage.text(String(score), { font: 'display', size: o.scoreSize || 24, weight: 600, color: neg ? T.danger : T.text, w: w - 20 }));
  return c;
};
// Screen root: full-size background board with a column flex (padding + gap), children centered horizontally.
storage.screen = (name, w, h, x, y, o = {}) => {
  const b = storage.col(name, w, h, { p: o.p ?? 24, pt: o.pt, pb: o.pb, px: o.px, py: o.py, gap: o.gap ?? 16, align: o.align || 'center', justify: o.justify || 'start', fill: storage.radial('#1B2A6B', '#0B1233', '#05081A'), clip: true });
  b.x = x; b.y = y; return b;
};
// Horizontal row with the given children; `justify` defaults to start.
storage.hrow = (name, w, h, children, o = {}) => { const r = storage.row(name, w, h, { gap: o.gap ?? 12, align: o.align || 'center', justify: o.justify || 'start', px: o.px, py: o.py }); children.forEach(c => storage.add(r, c, c && c.__lo)); return r; };
storage.vcol = (name, w, h, children, o = {}) => { const c = storage.col(name, w, h, { gap: o.gap ?? 12, align: o.align || 'start', justify: o.justify || 'start', p: o.p, px: o.px, py: o.py }); children.forEach(ch => storage.add(c, ch, ch && ch.__lo)); return c; };
// Question table: rows of [theme label | price plates].
storage.table = (name, themes, prices, o = {}) => {
  const cellW = o.cellW || 62, cellH = o.cellH || 48, gap = o.gap || 8, nameW = o.nameW || 0, rowGap = o.rowGap || gap;
  const w = nameW + (nameW ? gap : 0) + prices.length * cellW + (prices.length - 1) * gap;
  const h = themes.length * cellH + (themes.length - 1) * rowGap;
  const grid = storage.col(name, w, h, { gap: rowGap, align: 'start' });
  themes.forEach((t, r) => {
    const row = storage.row('row/' + t, w, cellH, { gap, align: 'center' });
    if (nameW && o.namePlate) storage.add(row, storage.plate(nameW, cellH, { variant: 'default', label: t, font: 'display', size: o.nameSize || 20, weight: 500, upper: true, name: 'theme/' + t }));
    else if (nameW) storage.add(row, storage.text(t, { font: o.nameFont || 'display', size: o.nameSize || 20, weight: 500, upper: true, color: o.nameColor || T.muted, w: nameW, h: cellH }));
    prices.forEach((p, c) => { const st = o.state ? o.state(r, c) : 'default'; storage.add(row, storage.plate(cellW, cellH, { variant: st, label: st === 'dim' ? '' : String(p), size: o.priceSize || Math.round(cellH * 0.42), name: `cell/${r}/${c}` })); });
    storage.add(grid, row);
  });
  return grid;
};
// Report geometry of a board's direct children (for verification).
storage.geom = (b) => b.children.map(c => ({ n: c.name, x: Math.round(c.parentX), y: Math.round(c.parentY), w: Math.round(c.width), h: Math.round(c.height) }));
return { fonts: { oswald: !!fonts.oswald, manrope: !!fonts.manrope }, helpers: Object.keys(storage).length };
