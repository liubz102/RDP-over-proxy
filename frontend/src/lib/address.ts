// Turning what the user types into an address field into host and port, and
// back. The Go side validates the result (model.Target, model.Proxy); this
// only splits the text the way model.ParseTarget does.

export interface HostPort {
  host: string;
  /** 0 when the text names no port; NaN when the port part is not a number. */
  port: number;
}

function parsePort(text: string): number {
  return /^\d{1,5}$/.test(text) ? Number(text) : Number.NaN;
}

/**
 * Splits "host", "host:port", "[IPv6]", "[IPv6]:port" and a bare IPv6
 * address. Surrounding spaces are ignored.
 */
export function splitHostPort(text: string): HostPort {
  const s = text.trim();
  if (s.startsWith("[")) {
    const end = s.indexOf("]");
    if (end < 0) return { host: s, port: 0 };
    const host = s.slice(1, end);
    const rest = s.slice(end + 1);
    if (rest === "") return { host, port: 0 };
    if (!rest.startsWith(":")) return { host: s, port: 0 };
    return { host, port: parsePort(rest.slice(1)) };
  }
  const first = s.indexOf(":");
  if (first < 0) return { host: s, port: 0 };
  // More than one colon and no brackets: a bare IPv6 address.
  if (s.indexOf(":", first + 1) >= 0) return { host: s, port: 0 };
  return { host: s.slice(0, first), port: parsePort(s.slice(first + 1)) };
}

/** The address as the user would type it: the port only when it is not defaultPort. */
export function joinHostPort(host: string, port: number, defaultPort: number): string {
  if (host === "") return "";
  const bracketed = host.includes(":") ? `[${host}]` : host;
  if (port === defaultPort || port === 0) return host;
  return `${bracketed}:${port}`;
}
