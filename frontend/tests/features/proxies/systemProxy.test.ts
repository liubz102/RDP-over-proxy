import { createInstance, type TFunction } from "i18next";
import { beforeAll, describe, expect, it } from "vitest";
import en from "../../../src/locales/en.json";
import zhCN from "../../../src/locales/zh-CN.json";
import type { RouteView, SystemProxyView } from "../../../src/api/backend";
import { proxyName } from "../../../src/features/proxies/names";
import { routeProxy, routeText, systemNow } from "../../../src/features/proxies/systemProxy";

const i18n = createInstance();
let t: TFunction;

beforeAll(async () => {
  t = await i18n.init({
    resources: { en: { translation: en }, "zh-CN": { translation: zhCN } },
    lng: "en",
    fallbackLng: "en",
    interpolation: { escapeValue: false },
  });
});

const settings = { autoDetect: false, script: "", proxy: "", bypass: "" };
const manual = { kind: "http", host: "127.0.0.1", port: 10809 };

describe("the built-in entries' names", () => {
  it("names them in the user's language", () => {
    expect(proxyName(t, { id: "system", name: "" })).toBe("Follow system proxy");
    expect(proxyName(t, { id: "direct", name: "" })).toBe("Direct");
    expect(proxyName(t, { id: "a1", name: "Office" })).toBe("Office");
  });
});

describe("systemNow", () => {
  const view = (v: Omit<Partial<SystemProxyView>, "settings"> & { settings?: Partial<typeof settings> }): SystemProxyView => ({
    manual: null,
    ...v,
    settings: { ...settings, ...v.settings },
  });

  it("says what Windows' setting is, the automatic configuration before the manual proxy", () => {
    expect(systemNow(t, null)).toBe("");
    expect(systemNow(t, view({}))).toBe("Now: no proxy is set, so directly");
    expect(systemNow(t, view({ settings: { proxy: "127.0.0.1:10809" }, manual }))).toBe("Now: 127.0.0.1:10809 (HTTP)");
    expect(systemNow(t, view({ settings: { proxy: "socks=[::1]:1080" }, manual: { kind: "socks", host: "::1", port: 1080 } }))).toBe(
      "Now: [::1]:1080 (SOCKS5)",
    );
    expect(systemNow(t, view({ settings: { autoDetect: true } }))).toContain("automatically detected; directly");
    expect(systemNow(t, view({ settings: { autoDetect: true, proxy: "127.0.0.1:10809" }, manual }))).toContain(
      "127.0.0.1:10809 (HTTP) when nothing is found",
    );
    expect(systemNow(t, view({ settings: { script: "http://127.0.0.1:10810/pac", proxy: "127.0.0.1:10809" }, manual }))).toContain(
      "setup script",
    );
    expect(systemNow(t, view({ settings: { proxy: "ftp=127.0.0.1:21" } }))).toContain("can't be used");
    expect(systemNow(t, view({ error: "access denied" }))).toContain("can't be read");
  });
});

describe("routes", () => {
  const through: RouteView = { kind: "socks", server: "127.0.0.1", port: 10808, by: "manual" };

  it("says where Windows' setting takes the connection", () => {
    expect(routeText(t, through, "pc.example.com")).toBe(
      "As Windows' proxy setting says: through 127.0.0.1:10808 (SOCKS5) to pc.example.com",
    );
    expect(routeText(t, { kind: "direct", by: "bypass" }, "pc.example.com")).toContain("one of the proxy's exceptions");
    expect(routeText(t, { kind: "direct", by: "none" }, "pc.example.com")).toContain("no proxy is set");
    expect(routeText(t, { kind: "direct", by: "config" }, "pc.example.com")).toContain("automatic configuration");
  });

  it("tests the server the setting names, or nothing when it goes directly", () => {
    expect(routeProxy(through)).toMatchObject({ kind: "socks", server: "127.0.0.1", port: 10808, name: "system", secret: "" });
    expect(routeProxy({ kind: "direct", by: "none" })).toBeNull();
  });
});
