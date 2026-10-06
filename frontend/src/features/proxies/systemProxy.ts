// The built-in entry that follows Windows' proxy setting: how the setting
// reads for people, and the proxy server to test when it names one.
import type { TFunction } from "i18next";
import type { Proxy, RouteView, SystemProxyView, SystemServer } from "../../api/backend";
import { joinHostPort } from "../../lib/address";
import { kindName } from "./names";
import { emptyOptions } from "./proxyForm";

/** A proxy server as people read it: "127.0.0.1:10809 (HTTP)". */
export function serverText(t: TFunction, s: SystemServer): string {
  return t("proxies.systemServer", { address: joinHostPort(s.host, s.port, -1), kind: kindName(t, s.kind) });
}

/**
 * What Windows' proxy setting is now, in a few words. The setup script and
 * detection come before the manual proxy, as for Windows' own programs.
 */
export function systemNow(t: TFunction, v: SystemProxyView | null): string {
  if (!v) return "";
  if (v.error) return t("proxies.systemNow.unreadable");
  const s = v.settings;
  const manual = v.manual ? serverText(t, v.manual) : "";
  if (s.script) return t("proxies.systemNow.script");
  if (s.autoDetect) return manual ? t("proxies.systemNow.detectOrManual", { server: manual }) : t("proxies.systemNow.detect");
  if (manual) return t("proxies.systemNow.manual", { server: manual });
  if (s.proxy) return t("proxies.systemNow.unusable");
  return t("proxies.systemNow.none");
}

/** How Windows' setting takes a connection to target, for the route check. */
export function routeText(t: TFunction, r: RouteView, target: string): string {
  const server = routeServer(r);
  if (!server) return t(`connections.test.systemDirect.${r.by}`, { target, defaultValue: t("connections.test.systemDirect.none", { target }) });
  return t("connections.test.systemVia", { server: serverText(t, server), target });
}

/** The server a route goes through; null when it goes directly. */
function routeServer(r: RouteView): SystemServer | null {
  if (r.kind === "direct" || !r.server || !r.port) return null;
  return { kind: r.kind, host: r.server, port: r.port };
}

/** The proxy to test a route through: the server Windows' setting names; null when it goes directly. */
export function routeProxy(r: RouteView): Proxy | null {
  const s = routeServer(r);
  if (!s) return null;
  return {
    schema: 1,
    id: "",
    name: "system",
    kind: s.kind,
    server: s.host,
    port: s.port,
    username: "",
    secret: "",
    options: emptyOptions,
    outbound: "",
  };
}
