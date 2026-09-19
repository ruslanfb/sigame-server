// Foundations page: palette, typography, hex plate variants, buzzer button states (flex layout, helpers v2).
// Run after 02-helpers.js. Requires the "Foundations" page to be ACTIVE (open it first in a separate call:
//   let p = penpotUtils.getPageByName('Foundations') || (p = penpot.createPage(), p.name = 'Foundations', p); penpot.openPage(p);).
const T = storage.T, S = storage, add = S.add;
const page = penpot.currentPage;
page.root.children.filter(c => c.name === 'Foundations').forEach(c => c.remove());
const W = 1600, CW = W - 96;
const board = S.col('Foundations', W, 1060, { p: 48, gap: 32, align: 'start', fill: S.solid(T.bg), clip: true });
board.x = 0; board.y = 0; page.root.appendChild(board);
add(board, S.text('SIGame — Foundations', { font: 'display', size: 40, weight: 600, upper: true, w: CW, name: 'title' }));
add(board, S.text('Тёмная студия «Миллионера»: индиго-градиенты, золото как единственный акцент, электрик-синий для интерактива, шестиугольные плашки.', { size: 16, color: T.muted, w: 900, lines: 2, name: 'subtitle' }));
const sw = [['bg',T.bg],['bg2',T.bg2],['surface',T.surface],['surface2',T.surface2],['surface3',T.surface3],['border',T.border],['borderStrong',T.borderStrong],['primary',T.primary],['primary2',T.primary2],['accent',T.accent],['accent2',T.accent2],['accent3',T.accent3],['danger',T.danger],['success',T.success],['warning',T.warning],['info',T.info],['text',T.text],['muted',T.muted]];
const pal = S.col('palette', CW, 2 * 116 + 16, { gap: 16, align: 'start' });
[0, 1].forEach(i => { const r = S.row('row', CW, 116, { gap: 20, align: 'start' }); sw.slice(i * 9, i * 9 + 9).forEach(([n, c]) => { const cell = S.col('swatch/' + n, 140, 116, { gap: 8, align: 'start' }); add(cell, S.rect('color', 140, 80, { radius: 12, fill: S.solid(c), stroke: S.line(T.border) })); add(cell, S.text(n + '  ' + c, { size: 12, color: T.muted, w: 140 })); add(r, cell); }); add(pal, r); });
add(board, pal);
const typo = S.col('typography', CW, 150, { gap: 12, align: 'start' });
add(typo, S.text('Oswald 600 — DISPLAY / ЦЕНЫ / ЗАГОЛОВКИ', { font: 'display', size: 40, weight: 600, upper: true, color: T.accent2, w: 1000 }));
add(typo, S.text('Manrope 600 — интерфейс, вопросы, подписи', { size: 24, weight: 600, w: 1000 }));
add(typo, S.text('Manrope 400 — текст вопроса читается с телефона и с 4 м от телевизора: 16 / 20 / 28 / 40 / 64 px', { size: 16, color: T.muted, w: 1000 }));
add(board, typo);
const plates = S.col('plates', CW, 130, { gap: 12, align: 'start' });
const pr = S.row('variants', CW, 88, { gap: 40, align: 'center' });
[['default', '100'], ['active', '500'], ['gold', '1 000'], ['dim', '']].forEach(([v, l]) => add(pr, S.plate(300, 88, { variant: v, label: l, size: 40 })));
add(plates, pr);
add(plates, S.text('Плашка: default · active (выбранная/играется) · gold (ответ/деньги/кнопка) · dim (сыграно)', { size: 14, color: T.muted, w: 1000 }));
add(board, plates);
const br = S.row('buzzer-states', CW, 180, { gap: 60, align: 'start' });
[['idle', 'кнопка'], ['armed', 'ждите…'], ['lit', 'ЖМИ!'], ['pressed', 'принято'], ['locked', 'блок 3 с']].forEach(([v, l]) => { const c = S.col('button/' + v, 240, 180, { gap: 10, align: 'start' }); add(c, S.plate(240, 140, { variant: v, label: l, size: 36 })); add(c, S.text(v, { size: 12, color: T.muted, w: 240 })); add(br, c); });
add(board, br);
storage.foundationsBoardId = board.id;
return { board: board.id, children: board.children.length };
