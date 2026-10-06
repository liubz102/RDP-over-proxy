import type { LogLine } from "../api/backend";

/** How many lines of the app's log are kept (app.logRecent). */
export const APP_LOG_LINES = 1000;

const seq = (l: LogLine) => l.seq ?? 0;

/**
 * The lines, in Seq order, with one more. The log read from the Go side and
 * the lines sent as events (app:log) travel separately, so a line may come
 * twice or later than a newer one; Seq says where it goes. Only the newest
 * APP_LOG_LINES are kept.
 */
export function insertLine(lines: LogLine[], line: LogLine): LogLine[] {
  // Most lines are the newest: look from the end.
  let i = lines.length;
  while (i > 0 && seq(lines[i - 1]) > seq(line)) i--;
  if (i > 0 && seq(lines[i - 1]) === seq(line)) return lines;
  const next = [...lines.slice(0, i), line, ...lines.slice(i)];
  return next.length > APP_LOG_LINES ? next.slice(next.length - APP_LOG_LINES) : next;
}

/** The log read from the Go side, plus the lines sent as events meanwhile. */
export function mergeLines(read: LogLine[], seen: LogLine[]): LogLine[] {
  let out = [...read].sort((a, b) => seq(a) - seq(b));
  for (const line of seen) out = insertLine(out, line);
  return out;
}

/** The levels from the least to the most serious (logging.Level*). */
const levels = ["debug", "info", "warn", "error"];

/** Whether a line is at least as serious as the level shown ("" for every line). */
export function atLeast(line: LogLine, level: string): boolean {
  return level === "" || levels.indexOf(line.level) >= levels.indexOf(level);
}

/**
 * A line's arguments as "key=value", in key order, for lines whose text
 * does not take them in. Values that are not text are written as JSON.
 */
export function argsText(args: Record<string, unknown> | null | undefined): string {
  return Object.keys(args ?? {})
    .sort()
    .map((k) => {
      const v = args?.[k];
      return `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`;
    })
    .join(" ");
}
