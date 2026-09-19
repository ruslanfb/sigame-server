// Table / TV screens (1920×1080) on page "Screens", row y = 1100 — flex layout (helpers v2):
// T01 Lobby · T02 Table · T03 Question · T04 Reveal · T05 Winner.
const T = storage.T, S = storage, add = S.add;
const page = penpot.currentPage;
page.root.children.filter(c => /^T\d\d /.test(c.name)).forEach(c => c.remove());
const W = 1920, H = 1080, GAP = 120, Y0 = 1100, P = 48, CW = W - 2 * P;
const boards = [];
const mk = (i, name) => { const b = S.screen(`T0${i} ${name}`, W, H, i * (W + GAP), Y0, { p: P, pt: 28, pb: 40, gap: 24, align: 'center' }); page.root.appendChild(b); boards.push(b); return b; };
const brand = (b, mid = '') => { const r = S.row('topbar', CW, 40, { justify: 'space-between', align: 'center' }); add(r, S.text('СВОЯ ИГРА', { font: 'display', size: 28, weight: 600, upper: true, color: T.accent2, letterSpacing: 4, w: 400, name: 'brand' })); if (mid) add(r, S.text(mid, { font: 'display', size: 32, weight: 500, upper: true, color: T.muted, align: 'center', w: 600, name: 'round' })); add(r, S.text('K7M3P', { font: 'display', size: 28, weight: 600, color: T.muted, letterSpacing: 4, align: 'right', w: 400, name: 'code' })); add(b, r); };
const header = (b, theme, price) => { const hdr = S.row('header', 720, 80, { gap: 20, justify: 'center' }); add(hdr, S.plate(520, 80, { variant: 'default', label: theme, size: 40, name: 'hex/theme' })); add(hdr, S.plate(180, 80, { variant: 'gold', label: price, size: 44, name: 'hex/price' })); add(b, hdr); };
const players = (b, active = -1, states = {}) => {
  add(b, S.spacer(CW, 1), { fillH: true });
  const names = ['Аня', 'Борис', 'Вера', 'Глеб'], scores = ['1 300', '−200', '800', '400'];
  const r = S.row('players', CW, 120, { gap: 24, justify: 'center', align: 'center' });
  names.forEach((n, i) => {
    const st = states[i]; const variant = i === active ? 'gold' : st === 'right' ? 'active' : 'default'; const dark = variant === 'gold';
    const pl = S.plate(400, 120, { variant, name: 'player/' + n });
    const col = S.col('info', 300, 100, { gap: 0, align: 'center', justify: 'center' });
    add(col, S.text(n, { font: 'display', size: 34, weight: 500, color: dark ? T.accentContrast : T.text, align: 'center', w: 300, h: 44 }));
    add(col, S.text(scores[i], { font: 'display', size: 40, weight: 600, color: dark ? T.accentContrast : scores[i].startsWith('−') ? T.danger : T.accent2, align: 'center', w: 300, h: 50 }));
    add(pl, col, { z: 1 });
    if (st === 'wrong') add(pl, S.dot(20, T.danger, 'state/wrong'), { abs: { x: 400 - 44, y: 16 }, z: 2 });
    add(r, pl);
  });
  add(b, r);
};
// T01 Lobby
{ const b = mk(1, 'Lobby'); brand(b);
  add(b, S.spacer(CW, 40));
  add(b, S.text('СВОЯ ИГРА', { font: 'display', size: 160, weight: 600, upper: true, color: T.accent2, w: CW, h: 190, align: 'center', letterSpacing: 12 }));
  add(b, S.text('Пятничная игра', { size: 40, weight: 600, w: CW, align: 'center' }));
  add(b, S.text('Подключайтесь: откройте адрес и введите код', { size: 28, color: T.muted, w: CW, align: 'center' }));
  const join = S.row('join', CW, 240, { gap: 48, justify: 'center', align: 'center' });
  add(join, S.plate(560, 120, { variant: 'gold', label: 'K7M3P', size: 72, name: 'hex/code' }));
  const qr = S.col('qr', 240, 240, { justify: 'center', align: 'center', fill: S.solid('#FFFFFF'), radius: 16 }); add(qr, S.text('QR', { font: 'display', size: 40, color: T.bg, w: 200, align: 'center' })); add(join, qr);
  add(b, join);
  add(b, S.text('http://192.168.1.5:8080/?room=K7M3P', { size: 28, color: T.primary2, w: CW, align: 'center' }));
  players(b, -1);
}
// T02 Table
{ const b = mk(2, 'Table'); brand(b, 'Раунд 1');
  add(b, S.table('table', ['История', 'Кино', 'Наука', 'Спорт', 'Музыка', 'География'], [100, 200, 300, 400, 500], { cellW: 230, cellH: 88, gap: 16, rowGap: 20, nameW: 420, namePlate: true, nameSize: 34, priceSize: 44, state: (r, c) => (r === 1 && c === 2) ? 'gold' : ((r + c) % 4 === 0) ? 'dim' : 'default' }));
  players(b, -1, { 0: 'right' });
}
// T03 Question
{ const b = mk(3, 'Question'); brand(b, 'Раунд 1'); header(b, 'КИНО', '300');
  const body = S.row('body', CW, 520, { gap: 40, align: 'center' });
  const media = S.col('media', 880, 520, { justify: 'center', align: 'center', fill: S.solid(T.bg2), stroke: S.line(T.border, 2), radius: 20 }); add(media, S.text('▶ медиа: картинка / видео / аудио', { size: 28, color: T.muted, w: 800, align: 'center' })); add(body, media);
  add(body, S.text('Этот режиссёр снял «Начало», «Интерстеллар» и «Оппенгеймер».', { size: 44, weight: 600, w: CW - 920, lines: 4, valign: 'center', h: 300 }));
  add(b, body);
  add(b, S.bar(CW - 384, 10, 0.62));
  add(b, S.plate(600, 90, { variant: 'gold', label: 'Отвечает Аня', size: 40, font: 'display', weight: 500, name: 'hex/answerer' }));
  players(b, 0, { 1: 'wrong' });
}
// T04 Reveal
{ const b = mk(4, 'Reveal'); brand(b, 'Раунд 1'); header(b, 'КИНО', '300');
  add(b, S.text('Этот режиссёр снял «Начало», «Интерстеллар» и «Оппенгеймер».', { size: 40, color: T.muted, w: CW - 400, lines: 2, align: 'center', valign: 'center', h: 110 }));
  add(b, S.text('ПРАВИЛЬНЫЙ ОТВЕТ', { font: 'display', size: 28, color: T.muted, upper: true, letterSpacing: 6, w: CW, align: 'center' }));
  add(b, S.plate(1040, 150, { variant: 'gold', label: 'Кристофер Нолан', size: 80, font: 'display', weight: 600, name: 'hex/answer' }));
  add(b, S.plate(440, 80, { variant: 'active', label: 'Аня: верно  +300', size: 34, font: 'display', weight: 500, name: 'hex/verdict' }));
  players(b, 0, { 1: 'wrong' });
}
// T05 Winner
{ const b = mk(5, 'Winner'); brand(b);
  add(b, S.spacer(CW, 80));
  add(b, S.text('ПОБЕДИТЕЛЬ', { font: 'display', size: 48, color: T.muted, upper: true, letterSpacing: 12, w: CW, h: 64, align: 'center' }));
  const wp = S.plate(1200, 240, { variant: 'gold', label: 'АНЯ', size: 160, name: 'hex/winner' });
  const bg = wp.children.find(c => c.name === 'bg'); if (bg) { bg.strokes = [S.line('#FFFFFF', 4)]; bg.shadows = [S.glow(T.accent, 120, 0.8)]; }
  add(b, wp);
  add(b, S.text('1 300 очков', { font: 'display', size: 64, weight: 500, color: T.accent2, w: CW, h: 84, align: 'center' }));
  players(b, 0);
}
storage.tableBoards = boards.map(b => ({ id: b.id, name: b.name }));
return boards.map(b => ({ name: b.name, children: b.children.length }));
