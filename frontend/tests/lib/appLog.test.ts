import { describe, expect, it } from "vitest";
import type { LogLine } from "../../src/api/backend";
import { APP_LOG_LINES, argsText, atLeast, insertLine, mergeLines } from "../../src/lib/appLog";

const line = (seq: number, level = "info"): LogLine => ({
  time: "2026-10-06T10:00:00Z",
  level,
  source: "app",
  msg: `line ${seq}`,
  seq,
});
const seqs = (lines: LogLine[]) => lines.map((l) => l.seq);

describe("insertLine", () => {
  it("puts each line where its Seq says, once", () => {
    let lines: LogLine[] = [];
    for (const n of [1, 2, 4, 3, 4, 2, 5]) lines = insertLine(lines, line(n));
    expect(seqs(lines)).toEqual([1, 2, 3, 4, 5]);
  });

  it("keeps the newest lines only", () => {
    let lines: LogLine[] = [];
    for (let n = 1; n <= APP_LOG_LINES + 5; n++) lines = insertLine(lines, line(n));
    expect(lines).toHaveLength(APP_LOG_LINES);
    expect(lines[0].seq).toBe(6);
    // A line older than all that are kept goes again at once.
    expect(insertLine(lines, line(2))).toHaveLength(APP_LOG_LINES);
    expect(insertLine(lines, line(2))[0].seq).toBe(6);
  });
});

describe("mergeLines", () => {
  it("adds the lines sent while the log was read, whichever came first", () => {
    expect(seqs(mergeLines([line(1), line(2), line(3)], [line(3), line(4)]))).toEqual([1, 2, 3, 4]);
    expect(seqs(mergeLines([line(2), line(1)], []))).toEqual([1, 2]);
  });
});

describe("atLeast", () => {
  it("filters by how serious a line is", () => {
    expect(atLeast(line(1, "debug"), "")).toBe(true);
    expect(atLeast(line(1, "info"), "warn")).toBe(false);
    expect(atLeast(line(1, "warn"), "warn")).toBe(true);
    expect(atLeast(line(1, "error"), "warn")).toBe(true);
    expect(atLeast(line(1, "warn"), "error")).toBe(false);
  });
});

describe("argsText", () => {
  it("writes the arguments in key order", () => {
    expect(argsText({ file: "a.json", code: "store.unreadable", n: 3, ok: true })).toBe(
      "code=store.unreadable file=a.json n=3 ok=true",
    );
    expect(argsText(undefined)).toBe("");
  });
});
