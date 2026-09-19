// Foundations page: palette, typography, hex plate variants, buzzer button states.
// Run after 02-helpers.js. Requires the "Foundations" page to be ACTIVE (open it first in a separate call:
//   let p = penpotUtils.getPageByName('Foundations') || (p = penpot.createPage(), p.name = 'Foundations', p); penpot.openPage(p);).
const T = storage.T;
const page = penpot.currentPage;
const old = page.root.children.find(c => c.name === 'Foundations'); if (old) old.remove();
const board = penpot.createBoard(); board.name = 'Foundations'; board.x = 0; board.y = 0; board.resize(1600, 1000);
board.fills = [{ fillColor: T.bg, fillOpacity: 1 }];
page.root.appendChild(board);
board.appendChild(storage.text('SIGame — Foundations', 48, 40, { font: 'display', size: 40, weight: 600, upper: true, name: 'title' }));
board.appendChild(storage.text('Тёмная студия «Миллионера»: индиго-градиенты, золото как единственный акцент, электрик-синий для интерактива, шестиугольные плашки.', 48, 96, { size: 16, color: T.muted, w: 900, name: 'subtitle' }));
const sw = [['bg',T.bg],['bg2',T.bg2],['surface',T.surface],['surface2',T.surface2],['surface3',T.surface3],['border',T.border],['borderStrong',T.borderStrong],['primary',T.primary],['primary2',T.primary2],['accent',T.accent],['accent2',T.accent2],['accent3',T.accent3],['danger',T.danger],['success',T.success],['warning',T.warning],['info',T.info],['text',T.text],['muted',T.muted]];
sw.forEach(([n, c], i) => {
  const x = 48 + (i % 9) * 160, y = 160 + Math.floor(i / 9) * 130;
  const r = penpot.createRectangle(); r.x = x; r.y = y; r.resize(140, 80); r.borderRadius = 12; r.fills = [{ fillColor: c, fillOpacity: 1 }]; r.strokes = [{ strokeColor: T.border, strokeOpacity: 1, strokeWidth: 1 }]; r.name = 'swatch/' + n; board.appendChild(r);
  board.appendChild(storage.text(n + '  ' + c, x, y + 88, { size: 12, color: T.muted, name: 'swatch-label' }));
});
board.appendChild(storage.text('Oswald 600 — DISPLAY / ЦЕНЫ / ЗАГОЛОВКИ', 48, 440, { font: 'display', size: 40, weight: 600, upper: true, color: T.accent2 }));
board.appendChild(storage.text('Manrope 600 — интерфейс, вопросы, подписи', 48, 500, { size: 24, weight: 600 }));
board.appendChild(storage.text('Manrope 400 — текст вопроса читается с телефона и с 4 м от телевизора: 16 / 20 / 28 / 40 / 64 px', 48, 540, { size: 16, color: T.muted, w: 900 }));
['default', 'active', 'gold', 'dim'].forEach((v, i) => storage.hexPlate(board, 48 + i * 340, 620, 300, 88, { variant: v, label: v === 'gold' ? '1 000' : v === 'dim' ? '300' : v === 'active' ? '500' : '100', size: 40 }));
board.appendChild(storage.text('Плашка: default · active (выбранная/играется) · gold (ответ/деньги/кнопка) · dim (сыграно)', 48, 730, { size: 14, color: T.muted }));
const states = [['idle','#2A3566',T.borderStrong],['armed',T.accent,T.accent2],['lit',T.accent2,'#FFFFFF'],['pressed',T.primary,T.primary2],['locked',T.danger,'#FF8A93']];
states.forEach(([n, c, s], i) => {
  const x = 48 + i * 300;
  const p = storage.hexPath(x, 790, 240, 140); p.name = 'button/' + n; p.fills = [n === 'idle' ? { fillColor: c, fillOpacity: 1 } : storage.vgrad(s, c)]; p.strokes = [{ strokeColor: s, strokeOpacity: 1, strokeWidth: 3, strokeAlignment: 'inner' }];
  if (n === 'lit' || n === 'armed') p.shadows = [{ style: 'drop-shadow', offsetX: 0, offsetY: 0, blur: 40, spread: 0, color: { color: T.accent, opacity: 0.6 } }];
  if (n === 'pressed') p.shadows = [{ style: 'drop-shadow', offsetX: 0, offsetY: 0, blur: 40, spread: 0, color: { color: T.primary, opacity: 0.6 } }];
  board.appendChild(p);
  const t = storage.text(n === 'lit' ? 'ЖМИ!' : n === 'armed' ? 'ждите…' : n === 'pressed' ? 'принято' : n === 'locked' ? 'блок 3 с' : 'кнопка', x, 790, { font: 'display', size: 36, weight: 600, color: (n === 'lit' || n === 'armed') ? T.accentContrast : T.text, align: 'center', w: 240, h: 140 }); t.verticalAlign = 'center'; board.appendChild(t);
  board.appendChild(storage.text(n, x, 940, { size: 12, color: T.muted }));
});
storage.foundationsBoardId = board.id;
return { board: board.id, children: board.children.length };
