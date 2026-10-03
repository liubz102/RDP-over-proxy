import { describe, expect, it } from "vitest";
import { toBase64 } from "../../src/lib/base64";

describe("toBase64", () => {
  it("encodes bytes as the Go side decodes a []byte", () => {
    expect(toBase64(new Uint8Array([]))).toBe("");
    expect(toBase64(new Uint8Array([0xff, 0xfe, 0x66, 0x00]))).toBe("//5mAA==");
  });

  it("encodes a file larger than one slice", () => {
    const bytes = new Uint8Array(0x8000 * 2 + 5);
    for (let i = 0; i < bytes.length; i++) bytes[i] = (i * 31) & 0xff;
    let binary = "";
    for (const b of bytes) binary += String.fromCharCode(b);
    expect(toBase64(bytes)).toBe(btoa(binary));
  });
});
