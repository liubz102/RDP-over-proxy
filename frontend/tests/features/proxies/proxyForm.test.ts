import { describe, expect, it } from "vitest";
import type { Proxy } from "../../../src/api/backend";
import {
  changeKind,
  changeNetwork,
  defaultOptions,
  emptyOptions,
  formField,
  fromForm,
  fromLink,
  hasAdvanced,
  sameSecret,
  sections,
  splitServer,
  toForm,
} from "../../../src/features/proxies/proxyForm";

const proxy: Proxy = {
  schema: 1,
  id: "0123456789abcdef",
  name: "Office",
  kind: "socks",
  server: "proxy.example.com",
  port: 1080,
  username: "alice",
  secret: "",
  options: emptyOptions,
  outbound: "",
};

const vless: Proxy = {
  ...proxy,
  name: "Tokyo",
  kind: "vless",
  port: 443,
  username: "",
  options: { ...defaultOptions("vless"), network: "ws", path: "/ray", security: "tls", sni: "cdn.example.com" },
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

  it("does not carry a secret over to another kind", () => {
    const f = changeKind(toForm(vless), "trojan");
    expect(fromForm(vless, f, true)).toMatchObject({ keepSecret: false, proxy: { kind: "trojan", secret: "" } });
  });

  it("keeps the settings of a V2Ray-family proxy", () => {
    const r = fromForm(vless, { ...toForm(vless), password: "b831381d-6324-4d53-ad4f-8cda48b30811" }, false);
    expect(r.proxy.options).toEqual(vless.options);
    expect(r.proxy.secret).toBe("b831381d-6324-4d53-ad4f-8cda48b30811");
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

  it("starts another kind's settings over and keeps a typed password", () => {
    const f = changeKind({ ...toForm(vless), password: "typed" }, "vmess");
    expect(f.options).toEqual(defaultOptions("vmess"));
    expect(f.password).toBe("typed");
    expect(changeKind(toForm(vless), "vless").options).toEqual(vless.options);
  });

  it("fills the form from a share link", () => {
    const link: Proxy = { ...vless, id: "", name: "From link", secret: "b831381d-6324-4d53-ad4f-8cda48b30811" };
    const fresh = fromLink(toForm(proxy), link, null, false);
    expect(fresh).toMatchObject({ name: "From link", kind: "vless", password: link.secret, clearSecret: false });
    expect(fresh.options).toEqual(vless.options);
    // Bringing an existing proxy up to date keeps its name.
    expect(fromLink(toForm(vless), link, vless, true).name).toBe("Tokyo");
    // A link without a password has none, rather than keeping the saved one.
    const open = fromLink(toForm(proxy), { ...proxy, username: "", secret: "" }, proxy, true);
    expect(fromForm(proxy, open, true)).toMatchObject({ keepSecret: false, proxy: { secret: "", username: "" } });
  });

  it("leaves the password field alone for a new proxy from a link without one", () => {
    // Nothing is saved, so nothing is cleared: the field stays open for typing.
    const f = fromLink(toForm({ ...proxy, id: "", username: "" }), { ...proxy, id: "", username: "", secret: "" }, null, false);
    expect(f.clearSecret).toBe(false);
    // Nor is a saved secret of another kind cleared: it is not kept anyway.
    expect(fromLink(toForm(vless), { ...proxy, secret: "" }, vless, true).clearSecret).toBe(false);
  });

  it("keeps a SOCKS5 password for HTTP, but no secret across other kinds", () => {
    expect(fromForm(proxy, { ...toForm(proxy), kind: "http" }, true).keepSecret).toBe(true);
    expect(sameSecret("socks", "http")).toBe(true);
    expect(sameSecret("vmess", "vless")).toBe(false);
    // Clearing the password carries over between SOCKS5 and HTTP only.
    const cleared = { ...toForm(proxy), clearSecret: true };
    expect(changeKind(cleared, "http").clearSecret).toBe(true);
    expect(changeKind(cleared, "trojan").clearSecret).toBe(false);
  });

  it("starts the mode and disguise over on another network", () => {
    const grpc = changeNetwork({ ...emptyOptions, network: "xhttp", mode: "stream-one" }, "grpc");
    expect(grpc).toMatchObject({ network: "grpc", mode: "gun", headerType: "" });
    expect(changeNetwork(grpc, "xhttp")).toMatchObject({ mode: "auto" });
    const kcp = changeNetwork({ ...emptyOptions, network: "tcp", headerType: "http" }, "kcp");
    expect(kcp).toMatchObject({ network: "kcp", headerType: "none", mode: "" });
    expect(changeNetwork(kcp, "kcp")).toBe(kcp);
  });

  it("maps the Go side's field names", () => {
    expect(formField("secret")).toBe("password");
    expect(formField("outbound")).toBe("outbound");
    expect(formField("options.publicKey")).toBe("options.publicKey");
    expect(formField("options.unknown")).toBeNull();
    expect(formField("id")).toBeNull();
  });

  it("shows the sections each kind has", () => {
    expect(sections(toForm(proxy))).toEqual({ account: true, secret: null, transport: false, tls: false, reality: false, custom: false });
    expect(sections(toForm(vless))).toMatchObject({ account: false, secret: "userId", transport: true, tls: true, reality: false });
    expect(sections({ kind: "vless", options: { ...vless.options, security: "reality" } })).toMatchObject({ tls: false, reality: true });
    expect(sections({ kind: "hysteria2", options: emptyOptions })).toMatchObject({ secret: "password", transport: false, tls: true });
    expect(sections({ kind: "shadowsocks", options: emptyOptions })).toMatchObject({ secret: "password", transport: false, tls: false });
    expect(sections({ kind: "xray", options: emptyOptions })).toMatchObject({ secret: null, custom: true });
  });

  it("unfolds the advanced settings when one has a value", () => {
    expect(hasAdvanced(vless.options)).toBe(false);
    expect(hasAdvanced({ ...vless.options, finalMask: "{}" })).toBe(true);
  });
});
