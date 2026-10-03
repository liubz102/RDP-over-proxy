import { createInstance } from "i18next";
import { beforeAll, describe, expect, it } from "vitest";
import en from "../../../src/locales/en.json";
import type { DiagItem } from "../../../src/api/backend";
import { byGroup, detailText, reportText, valueText } from "../../../src/features/diagnostics/report";

const i18n = createInstance();
beforeAll(async () => {
  await i18n.init({ resources: { en: { translation: en } }, lng: "en", interpolation: { escapeValue: false } });
});

const items: DiagItem[] = [
  { group: "versions", key: "app", status: "info", text: "0.1.0" },
  { group: "versions", key: "mstsc", status: "error", value: "missing" },
  {
    group: "defaults",
    key: "defaultRdp",
    status: "ok",
    value: "found",
    detail: "%USERPROFILE%\Documents\Default.rdp",
    private: true,
  },
  { group: "defaults", key: "gateway", status: "warn", value: "maybe", detail: "gw.example.com", private: true },
  { group: "credentials", key: "encryptionOracle", status: "info", value: "other", args: { value: "7" } },
  { group: "credentials", key: "savedCredentials", status: "warn", value: "limited", detail: "TERMSRV/pc.example.com" },
];

describe("diagnostics report", () => {
  it("keeps the groups in the order the Go side lists them", () => {
    expect(byGroup(items).map(([g, list]) => [g, list.length])).toEqual([
      ["versions", 2],
      ["defaults", 2],
      ["credentials", 2],
    ]);
  });

  it("translates values and fills in their arguments", () => {
    const t = i18n.t.bind(i18n);
    expect(valueText(t, items[0])).toBe("0.1.0");
    expect(valueText(t, items[4])).toBe("Unknown value: 7.");
    expect(detailText(t, items[3])).toBe("Gateway server: gw.example.com");
    expect(detailText(t, items[0])).toBe("");
  });

  it("copies everything but private details, marking what may get in the way", () => {
    const t = i18n.t.bind(i18n);
    const text = reportText(t, items);
    expect(text).toContain("[Versions]");
    expect(text).toContain("This app: 0.1.0");
    expect(text).toContain(`[x] Remote Desktop Connection (mstsc): ${en.diag.items.mstsc.missing}`);
    expect(text).toContain(`[!] RD Gateway: ${en.diag.items.gateway.maybe}`);
    expect(text).toContain("Servers the policy names: TERMSRV/pc.example.com");
    expect(text).not.toContain("gw.example.com");
    expect(text).not.toContain("Default.rdp\n");
    expect(text).not.toContain("%USERPROFILE%");
    expect(text).not.toContain("{{");
  });
});
