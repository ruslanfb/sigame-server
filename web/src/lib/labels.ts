/**
 * Russian UI strings for protocol enums. Everything user-facing lives here so
 * the screens stay free of literal copy for machine codes.
 */
import type { PlayerState, PressStatus, QSub, Quality, Role, Stage, StakeMode, TrustLevel } from '../ws/types.ts';

export const ROLE_LABEL: Record<Role, string> = {
  player: 'Игрок',
  viewer: 'Зритель',
  showman: 'Ведущий',
};

export const STAGE_LABEL: Record<Stage, string> = {
  lobby: 'Лобби',
  roundIntro: 'Начало раунда',
  selecting: 'Выбор вопроса',
  question: 'Вопрос',
  roundEnd: 'Конец раунда',
  finalThemes: 'Финал: выбор темы',
  gameEnd: 'Игра окончена',
};

export const SUB_LABEL: Record<QSub, string> = {
  announcing: 'Объявление',
  secretTransfer: 'Передача вопроса',
  priceSelect: 'Выбор стоимости',
  stakes: 'Ставки',
  content: 'Вопрос',
  buttonWait: 'Кнопка',
  answering: 'Ответ',
  validating: 'Проверка',
  hiddenAnswering: 'Письменный ответ',
  reveal: 'Правильный ответ',
  end: 'Конец вопроса',
};

export const TIMER_LABEL: Record<string, string> = {
  questionSelection: 'Выбор вопроса',
  themeSelection: 'Выбор темы',
  playerSelection: 'Выбор игрока',
  showmanDecision: 'Решение ведущего',
  buttonPressing: 'Кнопка',
  answering: 'Ответ',
  soloAnswering: 'Ответ',
  hiddenAnswering: 'Письменный ответ',
  stakeMaking: 'Ставка',
  content: 'Чтение',
  mediaFallback: 'Медиа',
  reflection: 'Пауза',
  round: 'Раунд',
  appellation: 'Апелляция',
};

export const PLAYER_STATE_LABEL: Record<PlayerState, string> = {
  none: '',
  answering: 'Отвечает',
  lost: 'Мимо',
  right: 'Верно',
  wrong: 'Неверно',
  hasAnswered: 'Ответил',
  pass: 'Пас',
};

export const STAKE_MODE_LABEL: Record<StakeMode, string> = {
  nominal: 'Номинал',
  stake: 'Ставка',
  allIn: 'Ва-банк',
  pass: 'Пас',
};

export const PRESS_STATUS_LABEL: Record<PressStatus, string> = {
  pending: 'Принято',
  rejected: 'Отклонено',
  duplicate: 'Повтор',
  late: 'Поздно',
  stale: 'Устарело',
  lockedOut: 'Блокировка',
  tooEarly: 'Слишком рано',
  falseStart: 'Фальстарт',
};

export const LOCKOUT_REASON_LABEL: Record<string, string> = {
  falseStart: 'Фальстарт',
  tooEarly: 'Слишком рано',
  misfire: 'Нажатие до сигнала',
  lockedOut: 'Блокировка',
};

export const QUALITY_LABEL: Record<Quality, string> = {
  unsynced: 'Нет синхронизации',
  good: 'Хорошо',
  fair: 'Средне',
  poor: 'Плохо',
};

export const TRUST_LABEL: Record<TrustLevel, string> = {
  full: 'Полное',
  conservative: 'Осторожное',
  serverOnly: 'Только сервер',
  arrivalOnly: 'По приходу',
};

export const SOURCE_LABEL: Record<string, string> = {
  client: 'клиент',
  clamped: 'ограничено',
  server: 'сервер',
  arrival: 'по приходу',
};

export const VALIDATION_SOURCE_LABEL: Record<string, string> = {
  showman: 'ведущий',
  ai: 'ИИ',
  auto: 'авто',
  appeal: 'апелляция',
};

export const QUESTION_TYPE_LABEL: Record<string, string> = {
  simple: 'Обычный',
  stake: 'Со ставкой',
  stakeAll: 'Ставки всех',
  secret: 'Кот в мешке',
  secretPublicPrice: 'Кот в мешке',
  secretNoQuestion: 'Кот в мешке',
  noRisk: 'Без риска',
  forAll: 'Для всех',
  custom: 'Особый',
};

export const CHOOSER_REASON_LABEL: Record<string, string> = {
  roundStart: 'начало раунда',
  rightAnswer: 'верный ответ',
  stakeWinner: 'победа в торгах',
  secretRecipient: 'получил кота',
  rotation: 'по очереди',
  appeal: 'апелляция',
  showman: 'решение ведущего',
};

export const SELECT_REASON_LABEL: Record<string, string> = {
  chooser: 'Кто выбирает вопрос?',
  staker: 'Кто делает ставку первым?',
  deleter: 'Кто убирает тему?',
  secretTransfer: 'Кому передать вопрос?',
};

export const STAKE_REASON_LABEL: Record<string, string> = {
  stake: 'Ваша ставка',
  secretPrice: 'Стоимость вопроса',
  hiddenStake: 'Тайная ставка',
};

/** REST problem codes (docs/api.md §4) → user copy. */
export const API_ERROR_LABEL: Record<string, string> = {
  badPassword: 'Неверный пароль',
  invalidHostToken: 'Ключ ведущего не подходит к этой комнате',
  banned: 'Вы заблокированы в этой комнате',
  nameTaken: 'Это имя уже занято',
  roomFull: 'В комнате нет свободных мест',
  badRole: 'Эта роль недоступна в комнате',
  joinClosed: 'Ведущий закрыл вход в комнату',
  packNotFound: 'Пак не найден',
};

export function apiErrorText(status: number, code: string | null, fallback: string): string {
  if (code && API_ERROR_LABEL[code]) return API_ERROR_LABEL[code];
  switch (status) {
    case 404:
      return 'Комната не найдена';
    case 410:
      return 'Комната закрыта';
    case 401:
      return 'Нужен ключ ведущего';
    case 413:
      return 'Файл слишком большой';
    case 415:
      return 'Неподдерживаемый формат';
    case 422:
      return fallback || 'Неверные данные';
    case 503:
      return 'Сервер сейчас недоступен';
    default:
      return fallback || 'Ошибка';
  }
}

/** WebSocket ERROR codes → user copy. */
export const WS_ERROR_LABEL: Record<string, string> = {
  notAllowed: 'Действие недоступно',
  badState: 'Сейчас это нельзя сделать',
  badArgument: 'Неверный параметр',
  unknownType: 'Неизвестная команда',
  rateLimited: 'Слишком часто',
  badPayload: 'Ошибка данных',
  badToken: 'Сессия недействительна',
};

export const ROOM_CLOSED_LABEL: Record<string, string> = {
  host: 'Ведущий закрыл комнату',
  ttl: 'Комната закрыта по таймауту',
  shutdown: 'Сервер остановлен',
};

/** Pluralised score with a thin space as thousands separator. */
export function fmtScore(v: number): string {
  const sign = v < 0 ? '−' : '';
  const s = Math.abs(v).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ' ');
  return sign + s;
}

export function fmtDelta(v: number): string {
  return (v > 0 ? '+' : v < 0 ? '−' : '') + fmtScore(Math.abs(v));
}

export function plural(n: number, one: string, few: string, many: string): string {
  const m10 = n % 10;
  const m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

/** "Раунд 2 · Название" — the name is omitted when it is just the number. */
export function roundTitle(index: number, name: string): string {
  const n = index + 1;
  const trimmed = (name ?? '').trim();
  if (!trimmed || trimmed === String(n) || /^раунд\s*\d*$/i.test(trimmed)) return `Раунд ${n}`;
  return `Раунд ${n} · ${trimmed}`;
}
