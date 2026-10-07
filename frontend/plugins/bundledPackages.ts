import fs from "node:fs";
import path from "node:path";
import type { Plugin } from "vite";
import { packageFolder } from "./packageFolder";

/**
 * Reports which npm packages' code is in the bundle, for the third-party
 * notices (tools/notices): `.vite/bundled-packages.json` in the output, the
 * packages' folders relative to the frontend folder. Only modules with code
 * left after tree-shaking count, so a package the app imports nothing from
 * is not listed. Bundled code whose origin cannot be told stops the build.
 */
export function bundledPackages(): Plugin {
  let root = "";
  return {
    name: "rdp-over-proxy:bundled-packages",
    apply: "build",
    configResolved(config) {
      root = config.root;
    },
    generateBundle(_, bundle) {
      const project = path.dirname(root);
      const isPackage = (dir: string) => fs.existsSync(path.join(root, dir, "package.json"));
      const dirs = new Set<string>();
      for (const chunk of Object.values(bundle)) {
        if (chunk.type !== "chunk") continue;
        for (const [id, module] of Object.entries(chunk.modules)) {
          if (module.renderedLength === 0) continue;
          const dir = packageFolder(id, root, project, isPackage);
          if (dir === undefined) this.error(`cannot tell which package bundled code comes from: ${JSON.stringify(id)}`);
          if (dir) dirs.add(dir);
        }
      }
      this.emitFile({
        type: "asset",
        fileName: ".vite/bundled-packages.json",
        source: `${JSON.stringify([...dirs].sort(), null, 2)}\n`,
      });
    },
  };
}
