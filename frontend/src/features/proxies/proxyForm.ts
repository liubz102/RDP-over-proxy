import type { Proxy } from "../../api/backend";
import { splitHostPort } from "../../lib/address";

/** The proxy editor's fields, as the user types them. */
export interface ProxyForm {
  name: string;
  kind: string;
  server: string;
  port: string;
  username: string;
  /** A new password; empty keeps the saved one unless clearSecret is set. */
  password: string;
  clearSecret: boolean;
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
  };
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
 * The proxy to save and whether to keep the stored password. hasSecret says
 * whether one is stored now.
 */
export function fromForm(
  base: Proxy,
  f: ProxyForm,
  hasSecret: boolean,
): { proxy: Proxy; keepSecret: boolean; problems: Record<string, string> } {
  const problems: Record<string, string> = {};
  const port = /^\s*\d+\s*$/.test(f.port) ? Number(f.port) : f.port.trim() === "" ? 0 : Number.NaN;
  if (Number.isNaN(port)) problems.port = "invalid";
  const keepSecret = hasSecret && !f.clearSecret && f.password === "";
  const proxy: Proxy = {
    ...base,
    name: f.name,
    kind: f.kind,
    server: f.server,
    port: Number.isNaN(port) ? 0 : port,
    username: f.username,
    // A cleared password is cleared, whatever was typed before.
    secret: keepSecret || f.clearSecret ? "" : f.password,
  };
  return { proxy, keepSecret, problems };
}

/** The form field a problem the Go side reported belongs to. */
export function formField(field: string): keyof ProxyForm | null {
  switch (field) {
    case "name":
    case "kind":
    case "server":
    case "port":
    case "username":
      return field;
    case "secret":
      return "password";
    default:
      return null;
  }
}
