import type { LogLine, SessionView } from "../api/backend";

/** How many lines of a session log are kept (api.sessionLogSize). */
const LOG_LINES = 500;

/** The message key that starts a session's log; the Go side clears the log then. */
const STARTING = "session.starting";

const seq = (l: LogLine | undefined) => l?.seq ?? 0;

/**
 * A log with one more line. A "starting" line begins a new session's log. A
 * line that is not newer than the last one kept (by its Seq) is already
 * there: the log read from the Go side and the events sent for its lines
 * travel separately, and either may arrive first.
 */
export function appendLog(lines: LogLine[] | undefined, line: LogLine): LogLine[] {
  if (seq(line) <= seq(lines?.at(-1))) return lines ?? [];
  if (line.msg === STARTING) return [line];
  const next = [...(lines ?? []), line];
  return next.length > LOG_LINES ? next.slice(next.length - LOG_LINES) : next;
}

/** The log read from the Go side, plus the lines sent as events that it does not have yet. */
export function mergeLog(read: LogLine[], seen: LogLine[] | undefined): LogLine[] {
  let out = read;
  for (const line of seen ?? []) out = appendLog(out, line);
  return out;
}

/** Whether a session is between Connect and its end. */
export function isActive(s: SessionView | undefined): boolean {
  return !!s && s.phase !== "ended";
}
