/**
 * Where the code of a bundled module comes from, for the third-party
 * notices: the folder of its npm package relative to `root`, the frontend
 * folder ("node_modules/react"); "" for the project's own files (anything
 * in `project`, the repository, outside node_modules); undefined when it
 * cannot be told.
 *
 * Modules the bundler adds itself have virtual ids that name their package
 * ("\0rolldown/runtime.js"); `isPackage` says whether a folder relative to
 * `root` holds an installed package.
 */
export function packageFolder(
  id: string,
  root: string,
  project: string,
  isPackage: (dir: string) => boolean,
): string | undefined {
  if (id.startsWith("\0")) {
    const m = /^\0((?:@[^/]+\/)?[^/?]+)\//.exec(id);
    if (!m) return undefined;
    const dir = `node_modules/${m[1]}`;
    return isPackage(dir) ? dir : undefined;
  }
  const file = posix(id.split("?")[0]);
  const at = file.lastIndexOf("/node_modules/");
  if (at >= 0) {
    const [first, second] = file.slice(at + "/node_modules/".length).split("/");
    const name = first.startsWith("@") ? `${first}/${second}` : first;
    return inside(root, `${file.slice(0, at)}/node_modules/${name}`);
  }
  return inside(project, file) === undefined ? undefined : "";
}

/** A path with forward slashes and a lower-case drive letter. */
function posix(p: string): string {
  return p.replace(/\\/g, "/").replace(/^([A-Za-z]):/, (_, d: string) => `${d.toLowerCase()}:`);
}

/** file relative to base when it is inside it. Windows paths ignore case. */
function inside(base: string, file: string): string | undefined {
  const b = `${posix(base).replace(/\/+$/, "")}/`;
  const f = posix(file);
  return f.toLowerCase().startsWith(b.toLowerCase()) ? f.slice(b.length) : undefined;
}
