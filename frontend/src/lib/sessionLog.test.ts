import { describe, expect, it } from "vitest";
import type { LogLine } from "../api/backend";
import { appendLog, mergeLog } from "./sessionLog";

const line = (seq: number, msg: string): LogLine => ({
  time: "2026-10-02T10:00:00Z",
  msg,
  level: "info",
  source: "session",
  profile: "p",
  seq,
});

describe("appendLog", () => {
  it("starts over at a session's first line", () => {
    const old = [line(1, "session.listening")];
    expect(appendLog(old, line(2, "session.starting"))).toHaveLength(1);
    expect(appendLog(old, line(2, "session.closing"))).toHaveLength(2);
  });

  it("keeps the latest 500 lines", () => {
    let log: LogLine[] = [];
    for (let i = 1; i <= 510; i++) log = appendLog(log, line(i, `m${i}`));
    expect(log).toHaveLength(500);
    expect(log[0].msg).toBe("m11");
  });

  it("drops a line it already has, such as an event arriving after the log was read", () => {
    const log = [line(1, "session.starting"), line(2, "session.listening")];
    expect(appendLog(log, line(2, "session.listening"))).toBe(log);
    expect(appendLog(log, line(1, "session.starting"))).toBe(log);
  });
});

describe("mergeLog", () => {
  const a = line(1, "session.starting");
  const b = line(2, "session.listening");
  const c = line(3, "session.checkPassed");

  it("adds lines that arrived before the log was read but are newer", () => {
    expect(mergeLog([a, b], [a, b, c])).toEqual([a, b, c]);
  });

  it("does not repeat lines the reply already has", () => {
    expect(mergeLog([a, b, c], [b, c])).toEqual([a, b, c]);
  });

  it("keeps the events when the log read was empty", () => {
    expect(mergeLog([], [a])).toEqual([a]);
  });

  it("follows a new session that started after the read", () => {
    const next = line(9, "session.starting");
    expect(mergeLog([a, b], [next])).toEqual([next]);
  });
});
