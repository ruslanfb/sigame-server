// Token set "sigame" — mirrors web/src/styles/tokens.css (dark Millionaire theme).
const catalog = penpot.library.local.tokens;
let set = catalog.sets.find(s => s.name === 'sigame');
if (!set) set = catalog.addSet({ name: 'sigame' });
if (!set.active) set.toggleActive();
const existing = new Set(set.tokens.map(t => t.name));
const add = (type, name, value) => { if (!existing.has(name)) { set.addToken({ type, name, value }); existing.add(name); } };
const colors = {
  'color.bg': '#070B1F', 'color.bg2': '#0E1533', 'color.surface': '#151F45', 'color.surface2': '#1D2A5C', 'color.surface3': '#26367A',
  'color.border': '#2F3F7A', 'color.borderStrong': '#4A5FB0', 'color.text': '#F4F6FF', 'color.muted': '#9DA8D6', 'color.textInverse': '#0A0F2A',
  'color.primary': '#3D63FF', 'color.primary2': '#6F8BFF', 'color.primaryContrast': '#FFFFFF',
  'color.accent': '#F5B700', 'color.accent2': '#FFD76A', 'color.accent3': '#B8860B', 'color.accentContrast': '#1A1200',
  'color.danger': '#FF4D5A', 'color.success': '#2FD67B', 'color.warning': '#FFB020', 'color.info': '#4CC9F0',
  'color.button.idle': '#2A3566', 'color.button.armed': '{color.accent}', 'color.button.lit': '{color.accent2}', 'color.button.pressed': '{color.primary}', 'color.button.locked': '{color.danger}',
  'color.hex.top': '#1E2C66', 'color.hex.bottom': '#0F1740', 'color.hexActive.top': '#2F4BB0', 'color.hexActive.bottom': '#1A2A75',
  'color.gold.top': '#FFE08A', 'color.gold.bottom': '#E39B00',
};
for (const [n, v] of Object.entries(colors)) add('color', n, v);
for (const [n, v] of Object.entries({ 'space.1': '4', 'space.2': '8', 'space.3': '12', 'space.4': '16', 'space.5': '24', 'space.6': '32', 'space.7': '48', 'space.8': '64' })) add('spacing', n, v);
for (const [n, v] of Object.entries({ 'radius.sm': '6', 'radius.md': '12', 'radius.lg': '20', 'radius.full': '9999' })) add('borderRadius', n, v);
for (const [n, v] of Object.entries({ 'text.xs': '12', 'text.sm': '14', 'text.md': '16', 'text.lg': '20', 'text.xl': '28', 'text.2xl': '40', 'text.3xl': '64' })) add('fontSizes', n, v);
add('fontFamilies', 'font.sans', 'Manrope'); add('fontFamilies', 'font.display', 'Oswald');
add('fontWeights', 'weight.regular', '400'); add('fontWeights', 'weight.semibold', '600'); add('fontWeights', 'weight.bold', '700');
add('borderWidth', 'border.1', '1'); add('borderWidth', 'border.2', '2'); add('opacity', 'opacity.dim', '0.45');
return { set: set.name, count: set.tokens.length };
