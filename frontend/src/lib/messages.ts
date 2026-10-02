// Turning what the Go side reports (error codes, notice codes, session log
// keys) into text in the user's language. The Go side sends stable codes plus
// the original English text; the text is kept as details.
import type { i18n as I18n } from "i18next";
import type { ErrorView, FieldError, LogLine, Notice } from "../api/backend";

type Translator = Pick<I18n, "t" | "exists">;

/** What an error says in the user's language; its own text when the code has no translation. */
export function errorText(i18n: Translator, e: Pick<ErrorView, "code" | "message" | "args"> | null | undefined): string {
  if (!e) return "";
  const key = `errors.${e.code}`;
  if (e.code !== "unknown" && i18n.exists(key)) return i18n.t(key, { ...e.args });
  return e.message || i18n.t("errors.unknown");
}

// Codes whose original text can help find the cause: what the network, the
// proxy or Windows said. For the others (a proxy still in use, a connection
// already running, …) the translation says everything.
const technical = ["net.", "probe.", "tunnel.", "proxy.", "credential.", "secret.", "unknown"];

/**
 * The original text of an error, worth showing as details: English and
 * technical, so only where the translation may leave something out.
 */
export function errorDetails(e: Pick<ErrorView, "code" | "message"> | null | undefined): string {
  if (!e || e.code === "proxy.inUse" || !technical.some((p) => e.code.startsWith(p))) return "";
  return e.message;
}

/** A notice in the user's language. */
export function noticeText(i18n: Translator, n: Notice): string {
  const key = `notices.${n.code}`;
  return i18n.exists(key) ? i18n.t(key, { ...n.args }) : (n.message ?? n.code);
}

/** The first problem reported for each field: field path → code. */
export function fieldCodes(fields: FieldError[] | null | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  for (const f of fields ?? []) {
    out[f.field] ??= f.code;
  }
  return out;
}

/** The message shown under a form field for a validation code. */
export function fieldText(i18n: Translator, code: string | undefined): string | undefined {
  if (!code) return undefined;
  const key = `fieldErrors.${code}`;
  return i18n.exists(key) ? i18n.t(key) : code;
}

/** The text of one log line: session lines are keys with arguments, other lines plain English. */
export function logText(i18n: Translator, line: LogLine): string {
  const key = `log.${line.msg}`;
  if (!i18n.exists(key)) return line.msg;
  const args: Record<string, unknown> = { ...line.args };
  if (typeof args.error === "string") {
    // errorArgs fill in the error's own message, such as the RD Gateway's name.
    const errorArgs = (args.errorArgs ?? undefined) as Record<string, string> | undefined;
    args.error = errorText(i18n, { code: String(args.code ?? "unknown"), message: args.error, args: errorArgs });
  }
  if (typeof args.step === "string" && i18n.exists(`steps.${args.step}`)) {
    args.step = i18n.t(`steps.${args.step}`);
  }
  if (typeof args.outcome === "string" && i18n.exists(`outcomes.${args.outcome}`)) {
    args.outcome = i18n.t(`outcomes.${args.outcome}`);
  }
  return i18n.t(key, args);
}

/** The original error text of a log line, when its translation leaves it out. */
export function logDetails(i18n: Translator, line: LogLine): string {
  const error = line.args?.error;
  if (typeof error !== "string") return "";
  const code = String(line.args?.code ?? "unknown");
  // Untranslated errors are shown as they are; repeating them adds nothing.
  if (code === "unknown" || !i18n.exists(`errors.${code}`)) return "";
  return error;
}
