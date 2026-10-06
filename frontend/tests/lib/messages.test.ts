import { createInstance } from "i18next";
import { beforeAll, describe, expect, it } from "vitest";
import en from "../../src/locales/en.json";
import zhCN from "../../src/locales/zh-CN.json";
import { editableKinds, networks, securities, transportName } from "../../src/features/proxies/names";
import { errorDetails, errorText, fieldCodes, linkNoteText, logDetails, logText, noticeText } from "../../src/lib/messages";

const i18n = createInstance();

beforeAll(async () => {
  await i18n.init({
    resources: { en: { translation: en }, "zh-CN": { translation: zhCN } },
    lng: "en",
    fallbackLng: "en",
    interpolation: { escapeValue: false },
  });
});

describe("errorText", () => {
  it("translates a known code with its args", () => {
    expect(errorText(i18n, { code: "proxy.inUse", message: "in use", args: { profiles: "A, B" } })).toContain("A, B");
  });

  it("falls back to the error's own text, then to a generic one", () => {
    expect(errorText(i18n, { code: "nope.unknownCode", message: "raw text" })).toBe("raw text");
    expect(errorText(i18n, { code: "unknown", message: "" })).toBe(i18n.t("errors.unknown"));
  });

  it("keeps details only where they add something", () => {
    expect(errorDetails({ code: "proxy.auth", message: "socks: server rejects account" })).toBe("socks: server rejects account");
    expect(errorDetails({ code: "cancelled", message: "context canceled" })).toBe("");
    expect(errorDetails({ code: "proxy.inUse", message: "the proxy is used by 1 connection(s)" })).toBe("");
    // What Windows said about its proxy setting, but for what the translation says in full.
    expect(errorDetails({ code: "sysproxy.scriptUnavailable", message: "the setup script could not be downloaded (WinHTTP error 12167)" })).toBe(
      "the setup script could not be downloaded (WinHTTP error 12167)",
    );
    expect(errorDetails({ code: "sysproxy.unusable", message: "Windows' proxy setting names no proxy server this app can use" })).toBe("");
    expect(errorDetails({ code: "session.ending", message: "still ending" })).toBe("");
  });
});

describe("logText", () => {
  it("translates the error, step and outcome in a session line", async () => {
    await i18n.changeLanguage("zh-CN");
    const line = {
      time: "2026-10-02T10:00:00Z",
      level: "error",
      source: "session",
      msg: "session.stepFailed",
      args: { step: "check", error: "socks: server rejects account", code: "proxy.auth" },
    };
    const text = logText(i18n, line);
    expect(text).toContain(zhCN.steps.check);
    expect(text).toContain(zhCN.errors.proxy.auth);
    expect(logDetails(i18n, line)).toBe("socks: server rejects account");
    expect(logText(i18n, { ...line, level: "info", msg: "session.ended", args: { outcome: "closed" } })).toContain(
      zhCN.outcomes.closed,
    );
    await i18n.changeLanguage("en");
  });

  it("fills in the error's own arguments", () => {
    const text = logText(i18n, {
      time: "2026-10-02T10:00:00Z",
      level: "error",
      source: "session",
      msg: "session.stepFailed",
      args: {
        step: "preflight",
        error: "Remote Desktop is set to connect through an RD Gateway",
        code: "gateway.used",
        errorArgs: { server: "gw.example.com" },
      },
    });
    expect(text).toContain("gw.example.com");
    expect(text).not.toContain("{{");
  });

  it("shows an untranslated error as it is, without repeating it as details", () => {
    const line = {
      time: "2026-10-02T10:00:00Z",
      level: "warn",
      source: "session",
      msg: "session.upstreamFailing",
      args: { error: "something odd", code: "unknown" },
    };
    expect(logText(i18n, line)).toContain("something odd");
    expect(logDetails(i18n, line)).toBe("");
  });

  it("leaves plain English lines alone", () => {
    expect(logText(i18n, { time: "", level: "info", source: "app", msg: "engine started" })).toBe("engine started");
  });
});

describe("noticeText", () => {
  it("fills in the file name", () => {
    expect(noticeText(i18n, { id: 1, level: "warn", code: "store.unreadable", args: { file: "x.json" } })).toContain("x.json");
  });
});

describe("linkNoteText", () => {
  it("fills in the note's arguments", () => {
    expect(linkNoteText(i18n, { code: "ports", args: { ports: "443,20000-30000", port: "443" } })).toContain("443,20000-30000");
    expect(linkNoteText(i18n, { code: "somethingNew" })).toBe("somethingNew");
  });
});

describe("transportName", () => {
  it("names what is not plain TCP", () => {
    const t = i18n.t.bind(i18n);
    expect(transportName(t, "ws", "tls")).toBe("WebSocket + TLS");
    expect(transportName(t, "tcp", "reality")).toBe("REALITY");
    expect(transportName(t, "tcp", "none")).toBe("");
    expect(transportName(t, "", "")).toBe("");
  });
});

describe("fieldCodes", () => {
  it("keeps the first problem of each field", () => {
    expect(
      fieldCodes([
        { field: "name", code: "required" },
        { field: "name", code: "too_long" },
        { field: "target.host", code: "invalid" },
      ]),
    ).toEqual({ name: "required", "target.host": "invalid" });
  });
});

describe("catalogs", () => {
  // Codes the frontend builds keys from; the Go side's own codes are checked in internal/app.
  const keys = [
    ...["preparing", "checking", "launching", "running", "ending", "ended"].map((p) => `phases.${p}`),
    ...["preflight", "route", "listen", "check", "credential", "launch", "run", "done"].map((s) => `steps.${s}`),
    ...["closed", "cancelled", "failed"].map((o) => `outcomes.${o}`),
    ...["preparing", "checking", "launching", "running", "connected", "upstreamFailing", "tunnelFailed", "ending", "failed"].map(
      (s) => `status.${s}`,
    ),
    ...["required", "invalid", "out_of_range", "too_long", "unsupported", "conflict"].map((c) => `fieldErrors.${c}`),
    ...["error", "warn", "info", "debug"].map((l) => `settings.logLevels.${l}`),
    ...editableKinds.map((k) => `proxies.kinds.${k}`),
    ...networks.map((n) => `proxies.networks.${n}`),
    ...securities.map((s) => `proxies.securities.${s}`),
    // The app's log: logging.Level* and logging.Source*, and the filter.
    ...["error", "warn", "info", "debug"].map((l) => `appLog.levels.${l}`),
    ...["app", "engine", "session", "ui"].map((s) => `appLog.sources.${s}`),
    ...["all", "warn", "error"].map((f) => `appLog.filters.${f}`),
    // What the status icons mean, for screen readers.
    ...["ok", "info", "warn", "error"].map((s) => `diag.statuses.${s}`),
    ...["running", "ok", "warn", "error", "skipped"].map((s) => `connections.test.states.${s}`),
    // Following Windows' proxy setting: sysproxy.By* for direct routes.
    ...["none", "bypass", "config"].map((b) => `connections.test.systemDirect.${b}`),
  ];

  it.each(["en", "zh-CN"])("%s has every key built from a code", (lng) => {
    const missing = keys.filter((k) => !i18n.exists(k, { lng, fallbackLng: false }));
    expect(missing).toEqual([]);
  });
});
