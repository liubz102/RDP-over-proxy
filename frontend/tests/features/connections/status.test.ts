import { describe, expect, it } from "vitest";
import type { SessionView } from "../../../src/api/backend";
import { sessionStatus } from "../../../src/features/connections/status";

const base: SessionView = {
  profileId: "p",
  phase: "running",
  step: "run",
  outcome: "",
  failure: null,
  failedStep: "",
  addr: "127.1.2.3:13389",
  pid: 42,
  exitCode: 0,
  conns: 1,
  upstream: "unknown",
  upstreamError: null,
  tunnelError: null,
  check: null,
};

describe("sessionStatus", () => {
  it("offers Connect when there is no session", () => {
    expect(sessionStatus(undefined)).toMatchObject({ tone: "idle", label: null, actions: "connect" });
  });

  it("offers Cancel while starting", () => {
    for (const phase of ["preparing", "checking", "launching"]) {
      expect(sessionStatus({ ...base, phase })).toMatchObject({ tone: "busy", label: `status.${phase}`, actions: "cancel" });
    }
  });

  it("says connected only once the target answered", () => {
    expect(sessionStatus(base)).toMatchObject({ tone: "ok", label: "status.running", actions: "running" });
    expect(sessionStatus({ ...base, upstream: "ok" })).toMatchObject({ label: "status.connected" });
  });

  it("warns when the route stops reaching the target", () => {
    const err = { code: "proxy.dropped", message: "x" };
    expect(sessionStatus({ ...base, upstream: "failing", upstreamError: err })).toMatchObject({
      tone: "warn",
      error: err,
      actions: "running",
    });
  });

  it("puts a broken tunnel first", () => {
    const err = { code: "net.accessDenied", message: "x" };
    expect(sessionStatus({ ...base, upstream: "failing", tunnelError: err })).toMatchObject({ tone: "error", error: err });
  });

  it("shows why a session failed, and offers Connect again", () => {
    const failure = { code: "proxy.auth", message: "x" };
    expect(
      sessionStatus({ ...base, phase: "ended", step: "done", outcome: "failed", failure, failedStep: "check" }),
    ).toMatchObject({ tone: "error", label: "status.failed", args: { step: "check" }, error: failure, actions: "connect" });
  });

  it("forgets sessions that ended normally", () => {
    for (const outcome of ["closed", "cancelled"]) {
      expect(sessionStatus({ ...base, phase: "ended", step: "done", outcome })).toMatchObject({ tone: "idle", label: null });
    }
  });

  it("waits while ending", () => {
    expect(sessionStatus({ ...base, phase: "ending" })).toMatchObject({ tone: "busy", actions: "ending" });
  });
});
