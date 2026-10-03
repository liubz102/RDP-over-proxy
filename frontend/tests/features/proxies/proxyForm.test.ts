import { describe, expect, it } from "vitest";
import type { Proxy } from "../../../src/api/backend";
import { formField, fromForm, splitServer, toForm } from "../../../src/features/proxies/proxyForm";

const proxy: Proxy = {
  schema: 1,
  id: "0123456789abcdef",
  name: "Office",
  kind: "socks",
  server: "proxy.example.com",
  port: 1080,
  username: "alice",
  secret: "",
  outbound: "",
};

describe("proxy form", () => {
  it("keeps the saved password when the field is left empty", () => {
    const r = fromForm(proxy, toForm(proxy), true);
    expect(r.keepSecret).toBe(true);
    expect(r.proxy).toEqual(proxy);
    expect(r.problems).toEqual({});
  });

  it("replaces the saved password with a new one", () => {
    const r = fromForm(proxy, { ...toForm(proxy), password: "new" }, true);
    expect(r.keepSecret).toBe(false);
    expect(r.proxy.secret).toBe("new");
  });

  it("clears the saved password on request", () => {
    const r = fromForm(proxy, { ...toForm(proxy), clearSecret: true }, true);
    expect(r).toMatchObject({ keepSecret: false, proxy: { secret: "" } });
  });

  it("clears it even when a password was typed before", () => {
    const r = fromForm(proxy, { ...toForm(proxy), password: "typed", clearSecret: true }, true);
    expect(r).toMatchObject({ keepSecret: false, proxy: { secret: "" } });
  });

  it("has nothing to keep for a new proxy", () => {
    expect(fromForm(proxy, toForm(proxy), false).keepSecret).toBe(false);
  });

  it("reports a port that is not a number, and leaves an empty one for the Go side", () => {
    expect(fromForm(proxy, { ...toForm(proxy), port: "socks" }, false).problems).toEqual({ port: "invalid" });
    expect(fromForm(proxy, { ...toForm(proxy), port: "" }, false)).toMatchObject({ problems: {}, proxy: { port: 0 } });
  });

  it("splits a pasted host:port", () => {
    const f = splitServer({ ...toForm(proxy), server: "127.0.0.1:10808", port: "" });
    expect(f).toMatchObject({ server: "127.0.0.1", port: "10808" });
    expect(splitServer({ ...toForm(proxy), server: "proxy.example.com" })).toMatchObject({ port: "1080" });
  });

  it("maps the Go side's field names", () => {
    expect(formField("secret")).toBe("password");
    expect(formField("outbound")).toBeNull();
  });
});
