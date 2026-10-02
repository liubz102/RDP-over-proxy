import { describe, expect, it } from "vitest";
import zhCN from "./zh-CN.json";
import en from "./en.json";

// Flattens {"a": {"b": "x"}} into ["a.b"] so the two catalogs can be compared.
function keys(obj: object, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([k, v]) =>
    typeof v === "object" && v !== null ? keys(v, `${prefix}${k}.`) : [`${prefix}${k}`],
  );
}

describe("locales", () => {
  it("zh-CN and en have exactly the same keys", () => {
    expect(keys(zhCN).sort()).toEqual(keys(en).sort());
  });

  it("no string is empty", () => {
    const empty = (obj: object) =>
      keys(obj).filter((path) => path.split(".").reduce<any>((o, k) => o[k], obj) === "");
    expect(empty(zhCN)).toEqual([]);
    expect(empty(en)).toEqual([]);
  });
});
