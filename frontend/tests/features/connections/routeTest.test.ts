import { createInstance } from "i18next";
import { beforeAll, describe, expect, it } from "vitest";
import en from "../../../src/locales/en.json";
import zhCN from "../../../src/locales/zh-CN.json";
import type { CheckView } from "../../../src/api/backend";
import { protocolInfo, protocolKeys, running, startTest, verdict, type RouteTest, type Step } from "../../../src/features/connections/routeTest";

const passed: Step = { kind: "passed", ms: 12 };
const failed: Step = { kind: "failed", error: { code: "probe.noAnswer", message: "closed" } };
const check: CheckView = { elapsedMs: 12, protocol: "HYBRID_EX" };

function test(proxy: Step, target: Step): RouteTest {
  return { proxy, target, check: target.kind === "passed" ? check : null };
}

describe("route test", () => {
  it("starts with the proxy skipped for a direct connection", () => {
    expect(startTest(true)).toEqual({ proxy: { kind: "skipped" }, target: { kind: "running" }, check: null });
    expect(startTest(false).proxy).toEqual({ kind: "running" });
    expect(running(startTest(true))).toBe(true);
  });

  it("concludes only once both steps are done", () => {
    expect(verdict(test({ kind: "running" }, passed))).toBeNull();
    expect(verdict(test(passed, { kind: "running" }))).toBeNull();
  });

  it("tells the proxy from what lies beyond it", () => {
    expect(verdict(test(passed, passed))).toEqual({ tone: "ok", key: "connections.test.verdict.ok" });
    expect(verdict(test({ kind: "skipped" }, passed))).toEqual({ tone: "ok", key: "connections.test.verdict.ok" });
    // The computer answered, so the test URL is what failed.
    expect(verdict(test(failed, passed))).toEqual({ tone: "warn", key: "connections.test.verdict.testUrlFailed" });
    expect(verdict(test(passed, failed))).toEqual({ tone: "error", key: "connections.test.verdict.beyondProxy" });
    expect(verdict(test(failed, failed))).toEqual({ tone: "error", key: "connections.test.verdict.proxy" });
    expect(verdict(test({ kind: "skipped" }, failed))).toEqual({ tone: "error", key: "connections.test.verdict.direct" });
  });

  it("blames the connection's settings, not the route, when they are the problem", () => {
    const missing: Step = { kind: "failed", error: { code: "profile.proxyMissing", message: "" } };
    expect(verdict(test(missing, missing))).toEqual({ tone: "error", key: "connections.test.verdict.settings" });
    const self: Step = { kind: "failed", error: { code: "session.loopbackDirect", message: "" } };
    expect(verdict(test({ kind: "skipped" }, self))).toEqual({ tone: "error", key: "connections.test.verdict.settings" });
  });

  it("explains the security the computer chose", () => {
    expect(protocolInfo({ elapsedMs: 1, protocol: "HYBRID_EX" })).toEqual({
      key: "connections.test.protocols.hybridEx",
      tone: "ok",
      name: "HYBRID_EX",
    });
    expect(protocolInfo({ elapsedMs: 1, protocol: "RDP" }).tone).toBe("warn");
    expect(protocolInfo({ elapsedMs: 1, protocol: "0x40" }).key).toBe("connections.test.protocols.other");
    expect(
      protocolInfo({ elapsedMs: 1, protocol: "", negotiationFailure: "SSL_NOT_ALLOWED_BY_SERVER" }),
    ).toEqual({ key: "connections.test.failures.sslNotAllowed", tone: "warn", name: "SSL_NOT_ALLOWED_BY_SERVER" });
    expect(protocolInfo({ elapsedMs: 1, protocol: "", negotiationFailure: "0x99" }).key).toBe(
      "connections.test.failures.other",
    );
  });

  describe("catalogs", () => {
    const i18n = createInstance();
    beforeAll(async () => {
      await i18n.init({ resources: { en: { translation: en }, "zh-CN": { translation: zhCN } }, lng: "en" });
    });
    const keys = [
      ...protocolKeys,
      ...["ok", "testUrlFailed", "direct", "beyondProxy", "proxy", "settings"].map((v) => `connections.test.verdict.${v}`),
    ];
    it.each(["en", "zh-CN"])("%s explains every result", (lng) => {
      expect(keys.filter((k) => !i18n.exists(k, { lng, fallbackLng: false }))).toEqual([]);
    });
  });
});
