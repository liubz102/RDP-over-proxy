import type { Proxy, ProxyOptions } from "../../api/backend";
import { splitHostPort } from "../../lib/address";

/** The proxy editor's fields, as the user types them. */
export interface ProxyForm {
  name: string;
  kind: string;
  server: string;
  port: string;
  username: string;
  /** A new password or user ID; empty keeps the saved one unless clearSecret is set. */
  password: string;
  clearSecret: boolean;
  /** The V2Ray family's settings (model.ProxyOptions); the Go side keeps those the kind uses. */
  options: ProxyOptions;
  /** A custom Xray outbound, as JSON text. */
  outbound: string;
}

/** The form's fields that a problem can belong to: the top-level ones, and "options.<name>". */
export type FieldKey = Exclude<keyof ProxyForm, "options"> | `options.${keyof ProxyOptions}`;

export const emptyOptions: ProxyOptions = {
  cipher: "",
  flow: "",
  encryption: "",
  obfsPassword: "",
  network: "",
  headerType: "",
  host: "",
  path: "",
  serviceName: "",
  authority: "",
  mode: "",
  seed: "",
  extra: "",
  finalMask: "",
  security: "",
  sni: "",
  alpn: "",
  fingerprint: "",
  pinnedCerts: "",
  verifyNames: "",
  ech: "",
  publicKey: "",
  shortId: "",
  spiderX: "",
  mldsa65Verify: "",
};

/** The settings a new proxy of the kind starts with, as the Go side would fill them in. */
export function defaultOptions(kind: string): ProxyOptions {
  switch (kind) {
    case "vmess":
      return { ...emptyOptions, cipher: "auto", network: "tcp", headerType: "none", security: "none" };
    case "vless":
      return { ...emptyOptions, encryption: "none", network: "tcp", headerType: "none", security: "none" };
    case "trojan":
      return { ...emptyOptions, network: "tcp", headerType: "none", security: "tls" };
    case "shadowsocks":
      return { ...emptyOptions, cipher: "aes-256-gcm" };
    default:
      return { ...emptyOptions };
  }
}

export function toForm(p: Proxy): ProxyForm {
  return {
    name: p.name,
    kind: p.kind,
    server: p.server,
    port: p.port ? String(p.port) : "",
    username: p.username,
    password: "",
    clearSecret: false,
    options: { ...emptyOptions, ...p.options },
    outbound: p.outbound,
  };
}

/**
 * Whether a secret of one kind means the same to the other: SOCKS5 and HTTP
 * accounts are alike; otherwise a secret belongs to its kind (a VMess user ID
 * is no Trojan password). The Go side keeps stored secrets by the same rule.
 */
export function sameSecret(a: string, b: string): boolean {
  const account = (k: string) => k === "socks" || k === "http";
  return a === b || (account(a) && account(b));
}

/**
 * The form after the user picks another kind: the settings start over from
 * that kind's defaults (they mean other things to other kinds), and a typed
 * password is kept.
 */
export function changeKind(f: ProxyForm, kind: string): ProxyForm {
  if (kind === f.kind) return f;
  return { ...f, kind, options: defaultOptions(kind), clearSecret: sameSecret(f.kind, kind) && f.clearSecret };
}

/**
 * The settings after the user picks another network: the mode and the
 * disguise start over, since gRPC's modes are not XHTTP's and TCP's
 * disguises are not mKCP's.
 */
export function changeNetwork(o: ProxyOptions, network: string): ProxyOptions {
  if (network === o.network) return o;
  const mode = network === "grpc" ? "gun" : network === "xhttp" ? "auto" : "";
  const headerType = network === "tcp" || network === "kcp" ? "none" : "";
  return { ...o, network, mode, headerType };
}

/**
 * The form filled in from a share link (ProxyService.ParseLink). When it
 * brings a saved proxy (base) up to date, the name the user gave stays, and
 * a link without a password clears the saved one of the same kind rather
 * than keeping it.
 */
export function fromLink(f: ProxyForm, link: Proxy, base: Proxy | null, hasSecret: boolean): ProxyForm {
  const next = toForm(link);
  next.password = link.secret;
  next.clearSecret = base !== null && hasSecret && sameSecret(base.kind, link.kind) && link.secret === "";
  if (base !== null && f.name.trim() !== "") next.name = f.name;
  return next;
}

/**
 * A "host:port" pasted into the server field, split into its two fields.
 * Anything else is left as typed.
 */
export function splitServer(f: ProxyForm): ProxyForm {
  const { host, port } = splitHostPort(f.server);
  if (port > 0 && host !== "") return { ...f, server: host, port: String(port) };
  return f;
}

/**
 * Whether the stored secret stays: there is one, of the same kind, and the
 * user neither typed a new one nor cleared it.
 */
export function keepsSecret(base: Proxy, f: ProxyForm, hasSecret: boolean): boolean {
  return hasSecret && sameSecret(base.kind, f.kind) && !f.clearSecret && f.password === "";
}

/**
 * The proxy to save and whether to keep the stored secret. hasSecret says
 * whether one is stored now.
 */
export function fromForm(
  base: Proxy,
  f: ProxyForm,
  hasSecret: boolean,
): { proxy: Proxy; keepSecret: boolean; problems: Partial<Record<FieldKey, string>> } {
  const problems: Partial<Record<FieldKey, string>> = {};
  const port = /^\s*\d+\s*$/.test(f.port) ? Number(f.port) : f.port.trim() === "" ? 0 : Number.NaN;
  if (Number.isNaN(port)) problems.port = "invalid";
  const keepSecret = keepsSecret(base, f, hasSecret);
  const proxy: Proxy = {
    ...base,
    name: f.name,
    kind: f.kind,
    server: f.server,
    port: Number.isNaN(port) ? 0 : port,
    username: f.username,
    // A cleared password is cleared, whatever was typed before.
    secret: keepSecret || f.clearSecret ? "" : f.password,
    options: { ...f.options },
    outbound: f.outbound,
  };
  return { proxy, keepSecret, problems };
}

/** The form field a problem the Go side reported belongs to. */
export function formField(field: string): FieldKey | null {
  switch (field) {
    case "name":
    case "kind":
    case "server":
    case "port":
    case "username":
    case "outbound":
      return field;
    case "secret":
      return "password";
  }
  if (field.startsWith("options.") && field.slice("options.".length) in emptyOptions) return field as FieldKey;
  return null;
}

/** Settings few links carry and fewer people change; the editor folds them away. */
export const advancedOptions: readonly (keyof ProxyOptions)[] = [
  "authority",
  "extra",
  "finalMask",
  "verifyNames",
  "ech",
  "spiderX",
  "mldsa65Verify",
];

/** Whether any of the advanced settings has a value, so the editor shows them unfolded. */
export function hasAdvanced(o: ProxyOptions): boolean {
  return advancedOptions.some((k) => o[k] !== "");
}

/** What the editor shows for a kind and its settings. */
export interface Sections {
  /** A SOCKS5 or HTTP account: user name and password. */
  account: boolean;
  /** The one secret of the V2Ray family: "userId" for VMess and VLESS, "password" otherwise. */
  secret: "userId" | "password" | null;
  /** The transport and its security (VMess, VLESS, Trojan). */
  transport: boolean;
  /** TLS settings: TLS chosen, or Hysteria2, which is always TLS. */
  tls: boolean;
  reality: boolean;
  /** A custom outbound in JSON. */
  custom: boolean;
}

export function sections(f: Pick<ProxyForm, "kind" | "options">): Sections {
  const transport = f.kind === "vmess" || f.kind === "vless" || f.kind === "trojan";
  let secret: Sections["secret"] = null;
  if (f.kind === "vmess" || f.kind === "vless") secret = "userId";
  else if (f.kind === "trojan" || f.kind === "shadowsocks" || f.kind === "hysteria2") secret = "password";
  return {
    account: f.kind === "socks" || f.kind === "http",
    secret,
    transport,
    tls: f.kind === "hysteria2" || (transport && f.options.security === "tls"),
    reality: transport && f.options.security === "reality",
    custom: f.kind === "xray",
  };
}
