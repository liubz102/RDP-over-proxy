import { describe, expect, it } from "vitest";
import { THIRD_PARTY_URL, loadThirdParty } from "../../src/lib/thirdParty";

const answer = (body: string, init: ResponseInit) => async (url: string) => {
  expect(url).toBe(THIRD_PARTY_URL);
  return new Response(body, init);
};

describe("loadThirdParty", () => {
  it("reads the notices the build put next to the page", async () => {
    const got = await loadThirdParty(answer("RDP over Proxy - Third-party software", { headers: { "content-type": "text/plain; charset=utf-8" } }));
    expect(got).toBe("RDP over Proxy - Third-party software");
  });

  it("finds none where a development server answers with the page", async () => {
    expect(await loadThirdParty(answer("<!DOCTYPE html>", { headers: { "content-type": "text/html" } }))).toBeUndefined();
  });

  it("finds none when the file is missing", async () => {
    expect(await loadThirdParty(answer("not found", { status: 404, headers: { "content-type": "text/plain" } }))).toBeUndefined();
  });
});
