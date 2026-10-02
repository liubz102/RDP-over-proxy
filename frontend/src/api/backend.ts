// One place to import the generated Wails bindings from, so the long generated
// paths stay out of the rest of the code.
import type { ErrorView } from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/api";

export {
  AppService,
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
  LatencyResult,
  Notice,
  ProfileView,
  ProxyView,
  QuitView,
  SessionView,
} from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/api";
export type { Line as LogLine } from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/logging";
export type {
  Display,
  FieldError,
  Profile,
  Proxy,
  Settings,
  Target,
} from "../../bindings/github.com/liubz102/RDP-over-proxy/internal/model";

// Event names emitted by the Go side (internal/api).
export const Events = {
  settingsChanged: "settings:changed",
  dataChanged: "data:changed",
  sessionsChanged: "sessions:changed",
  sessionLog: "session:log",
  notice: "app:notice",
  quitRequested: "app:quitRequested",
} as const;

/** The built-in "no proxy" entry (model.DirectProxyID). */
export const DIRECT_PROXY_ID = "direct";

/** The standard Remote Desktop port (model.DefaultRDPPort). */
export const RDP_PORT = 3389;

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
