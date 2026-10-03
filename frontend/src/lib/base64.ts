/** Base64 of bytes: how the Go side takes a []byte. */
export function toBase64(bytes: Uint8Array): string {
  let s = "";
  // In slices: spreading a whole file into one call could exceed the
  // engine's limit on arguments.
  for (let i = 0; i < bytes.length; i += 0x8000) {
    s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(s);
}
