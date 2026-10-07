/** Where the build puts the third-party notices (tools/notices), next to the page. */
export const THIRD_PARTY_URL = "/THIRD_PARTY_NOTICES.txt";

/**
 * The third-party notices of this build, or undefined when it has none: a
 * development server answers an unknown path with the page itself.
 */
export async function loadThirdParty(
  get: (url: string) => Promise<Response> = (url) => fetch(url),
): Promise<string | undefined> {
  const res = await get(THIRD_PARTY_URL);
  if (!res.ok || !(res.headers.get("content-type") ?? "").startsWith("text/plain")) return undefined;
  return res.text();
}
