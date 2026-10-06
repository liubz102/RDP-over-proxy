// Proxies already running on this computer, as the empty pages offer them:
// which candidates (ProxyService.LocalProxies) to offer, once a program's
// port has answered as SOCKS5 (ProxyService.ProbeLocal), and the proxy to
// store for one.
import { LOCAL_SOURCE, type LocalCandidate, type LocalProbe, type Proxy } from "../../api/backend";
import { emptyOptions } from "./proxyForm";

/** A proxy on this computer, ready to add. */
export interface LocalOffer {
  /** Where the candidate was in the Go side's list, which sets the order. */
  order: number;
  /** The program, such as "v2rayN"; "" when not known. */
  program: string;
  host: string;
  port: number;
  kind: "socks" | "http";
  /** The SOCKS5 server wants a user name and password. */
  password: boolean;
  /** Found in Windows' Internet settings rather than by its program. */
  system: boolean;
}

/**
 * What a candidate offers: a program's port once it has answered as a
 * SOCKS5 server; the proxy in the Internet settings as HTTP, as Windows
 * uses it. Nothing for a port that answered otherwise.
 */
export function offerOf(order: number, c: LocalCandidate, probe?: LocalProbe): LocalOffer | null {
  const base = { order, program: c.name, port: c.port, password: false };
  if (c.source === LOCAL_SOURCE.system) {
    const host = c.hosts?.[0];
    return host ? { ...base, host, kind: "http", system: true } : null;
  }
  if (!probe?.socks5) return null;
  return { ...base, host: probe.host, kind: "socks", password: probe.password, system: false };
}

/** The offers in the Go side's order, without a second one for the same address. */
export function addOffer(offers: LocalOffer[], offer: LocalOffer | null): LocalOffer[] {
  if (!offer || offers.some((o) => o.host === offer.host && o.port === offer.port)) return offers;
  return [...offers, offer].sort((a, b) => a.order - b.order);
}

/**
 * The name of the proxy for an offer, from the translated pattern ("Local
 * {{name}}"). The port tells two offers of one program apart.
 */
export function offerName(offer: LocalOffer, offers: LocalOffer[], named: (program: string) => string): string {
  const name = named(offer.program);
  const twin = offers.some((o) => o !== offer && o.program === offer.program);
  return twin ? `${name} ${offer.port}` : name;
}

/** The proxy to store for an offer, with no account yet. */
export function proxyOf(offer: LocalOffer, name: string): Proxy {
  return {
    schema: 1,
    id: "",
    name,
    kind: offer.kind,
    server: offer.host,
    port: offer.port,
    username: "",
    secret: "",
    options: emptyOptions,
    outbound: "",
  };
}
