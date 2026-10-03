// The route test: the proxy on its own (a request to the test URL through
// it), and the remote computer through the proxy (an RDP connection
// request). Each step's result tells the other's apart: a proxy that works
// on its own but does not reach the computer points past the proxy.
import type { CheckView, ErrorView } from "../../api/backend";

export type Step =
  | { kind: "skipped" }
  | { kind: "running" }
  | { kind: "passed"; ms: number }
  | { kind: "failed"; error: ErrorView };

export interface RouteTest {
  /** The proxy on its own; skipped for a direct connection. */
  proxy: Step;
  /** The remote computer's Remote Desktop service, through the proxy. */
  target: Step;
  /** The computer's answer, once it came. */
  check: CheckView | null;
}

export function startTest(direct: boolean): RouteTest {
  return { proxy: direct ? { kind: "skipped" } : { kind: "running" }, target: { kind: "running" }, check: null };
}

export function running(test: RouteTest): boolean {
  return test.proxy.kind === "running" || test.target.kind === "running";
}

export type Tone = "ok" | "warn" | "error";

// Errors that come from the connection's own settings, not from the route:
// testing tells nothing about the network then.
const settingsCodes = new Set([
  "profile.proxyMissing",
  "proxy.secretsLost",
  "proxy.config",
  "proxy.unsupported",
  "store.notFound",
  "session.loopbackDirect",
  "validation",
]);

function settingsProblem(step: Step): boolean {
  return step.kind === "failed" && settingsCodes.has(step.error.code);
}

/** What the steps add up to, once they are done: a tone and a translation key. */
export function verdict(test: RouteTest): { tone: Tone; key: string } | null {
  if (running(test)) return null;
  if (settingsProblem(test.proxy) || settingsProblem(test.target)) {
    return { tone: "error", key: "connections.test.verdict.settings" };
  }
  if (test.target.kind === "passed") {
    // The computer answered; the test URL may just be blocked where the proxy is.
    if (test.proxy.kind === "failed") return { tone: "warn", key: "connections.test.verdict.testUrlFailed" };
    return { tone: "ok", key: "connections.test.verdict.ok" };
  }
  switch (test.proxy.kind) {
    case "skipped":
      return { tone: "error", key: "connections.test.verdict.direct" };
    case "passed":
      return { tone: "error", key: "connections.test.verdict.beyondProxy" };
    default:
      return { tone: "error", key: "connections.test.verdict.proxy" };
  }
}

// The security protocols and refusals a server answers with (probe.Protocols,
// probe.FailureCode), and the keys explaining them.
const protocols: Record<string, { key: string; tone: Tone }> = {
  HYBRID_EX: { key: "hybridEx", tone: "ok" },
  HYBRID: { key: "hybrid", tone: "ok" },
  SSL: { key: "ssl", tone: "ok" },
  RDSTLS: { key: "rdstls", tone: "ok" },
  RDSAAD: { key: "rdsaad", tone: "ok" },
  RDP: { key: "rdp", tone: "warn" },
};

const failures: Record<string, string> = {
  SSL_REQUIRED_BY_SERVER: "sslRequired",
  SSL_NOT_ALLOWED_BY_SERVER: "sslNotAllowed",
  SSL_CERT_NOT_ON_SERVER: "sslCertMissing",
  INCONSISTENT_FLAGS: "inconsistentFlags",
  HYBRID_REQUIRED_BY_SERVER: "hybridRequired",
  SSL_WITH_USER_AUTH_REQUIRED_BY_SERVER: "sslWithUserAuthRequired",
};

/** The keys the explanations use, for checking the catalogs. */
export const protocolKeys = [
  ...Object.values(protocols).map((p) => `connections.test.protocols.${p.key}`),
  "connections.test.protocols.other",
  ...Object.values(failures).map((f) => `connections.test.failures.${f}`),
  "connections.test.failures.other",
];

/**
 * What the security the computer chose means for signing in: a translation
 * key, with the protocol's name as "name". A refusal of every protocol
 * offered still proves the route.
 */
export function protocolInfo(c: CheckView): { key: string; tone: Tone; name: string } {
  if (c.negotiationFailure) {
    const f = failures[c.negotiationFailure];
    return { key: `connections.test.failures.${f ?? "other"}`, tone: "warn", name: c.negotiationFailure };
  }
  const p = protocols[c.protocol];
  return p
    ? { key: `connections.test.protocols.${p.key}`, tone: p.tone, name: c.protocol }
    : { key: "connections.test.protocols.other", tone: "ok", name: c.protocol };
}
