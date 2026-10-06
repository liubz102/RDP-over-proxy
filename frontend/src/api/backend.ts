// One place to import the generated Wails bindings from, so the long generated
// paths stay out of the rest of the code.
import type { ErrorView } from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/api";

export {
  AppService,
  DiagService,
  ProfileService,
  ProxyService,
  SessionService,
  SettingsService,
} from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/api";
export type {
  AppInfo,
  CheckView,
  ConnectResult,
  DataView,
  ErrorView,
  Folders,
  LatencyResult,
  LinkView,
  Notice,
  ProfileView,
  ProxyView,
  QuitView,
  RDPImportView,
  SessionView,
} from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/api";
export type { Item as DiagItem } from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/diag";
export type {
  Candidate as LocalCandidate,
  Result as LocalProbe,
} from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/localproxy";
export type { Line as LogLine } from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/logging";
export type {
  Display,
  FieldError,
  Profile,
  Proxy,
  ProxyOptions,
  Settings,
  Target,
} from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/model";
export type { Note as LinkNote } from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/sharelink";

// Event names emitted by the Go side (internal/api).
export const Events = {
  settingsChanged: "settings:changed",
  dataChanged: "data:changed",
  sessionsChanged: "sessions:changed",
  sessionLog: "session:log",
  notice: "app:notice",
  quitRequested: "app:quitRequested",
  diagChanged: "diag:changed",
  appLog: "app:log",
} as const;

/** The folders AppService.OpenFolder opens (api.FolderData, api.FolderLogs). */
export type FolderName = "data" | "logs";

/** The built-in "no proxy" entry (model.DirectProxyID). */
export const DIRECT_PROXY_ID = "direct";

/** The standard Remote Desktop port (model.DefaultRDPPort). */
export const RDP_PORT = 3389;

/** The largest file taken for an .rdp file (rdpfile.MaxSize). */
export const RDP_MAX_SIZE = 1 << 20;

/** Where a local proxy candidate was found (localproxy.SourceProgram, localproxy.SourceSystem). */
export const LOCAL_SOURCE = { program: "program", system: "system" } as const;

/**
 * The structured error a rejected service call carries in its `cause`
 * (internal/api.MarshalError): translate `errors.<code>`, show `message` as
 * the details, and mark `fields` in forms.
 */
export function errorOf(e: unknown): ErrorView {
  const cause = (e as { cause?: unknown } | null)?.cause;
  if (cause !== null && typeof cause === "object" && typeof (cause as { code?: unknown }).code === "string") {
    return cause as ErrorView;
  }
  return { code: "unknown", message: e instanceof Error ? e.message : String(e) };
}
