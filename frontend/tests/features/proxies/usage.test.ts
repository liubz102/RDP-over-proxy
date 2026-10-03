import { describe, expect, it } from "vitest";
import type { ProfileView, SessionView } from "../../../src/api/backend";
import { profileNames, proxyUsage } from "../../../src/features/proxies/usage";

function view(id: string, name: string, proxyId: string): ProfileView {
  return {
    profile: {
      schema: 1,
      id,
      name,
      group: "",
      target: { host: "pc.example.com", port: 3389 },
      proxyId,
      loopback: "127.1.2.3",
      username: "",
      rememberPassword: true,
      display: { mode: "default", width: 1280, height: 800, multimon: false, span: false },
      admin: false,
    },
    passwordSaved: false,
    passwordByMstsc: false,
    proxyMissing: false,
  };
}

function session(profileId: string, phase: string): SessionView {
  return {
    profileId,
    phase,
    step: phase === "ended" ? "done" : "run",
    outcome: phase === "ended" ? "closed" : "",
    failure: null,
    failedStep: "",
    addr: "",
    pid: 0,
    exitCode: 0,
    conns: 0,
    upstream: "unknown",
    upstreamError: null,
    tunnelError: null,
    check: null,
  };
}

const profiles = [view("a", "PC one", "office"), view("b", "PC two", "office"), view("c", "PC three", "home")];

describe("proxyUsage", () => {
  it("lists every profile set to the proxy, and the connected ones", () => {
    const u = proxyUsage("office", profiles, { a: session("a", "running"), c: session("c", "running") });
    expect(u.users.map((p) => p.profile.id)).toEqual(["a", "b"]);
    expect(u.connected.map((p) => p.profile.id)).toEqual(["a"]);
  });

  it("counts a session as connected until it has ended", () => {
    for (const phase of ["preparing", "checking", "launching", "running", "ending"]) {
      expect(proxyUsage("office", profiles, { b: session("b", phase) }).connected).toHaveLength(1);
    }
    expect(proxyUsage("office", profiles, { b: session("b", "ended") }).connected).toHaveLength(0);
  });

  it("finds nobody for a proxy no profile uses", () => {
    expect(proxyUsage("unused", profiles, {})).toEqual({ users: [], connected: [] });
  });
});

describe("profileNames", () => {
  it("joins the names in list order", () => {
    expect(profileNames(profiles, "、")).toBe("PC one、PC two、PC three");
    expect(profileNames([], ", ")).toBe("");
  });
});
