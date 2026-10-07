import { describe, expect, it } from "vitest";
import { packageFolder } from "../../plugins/packageFolder";

const root = "D:/git/app/frontend";
const project = "D:/git/app";
const installed = new Set(["node_modules/rolldown", "node_modules/@scope/helpers"]);
const isPackage = (dir: string) => installed.has(dir);

describe("packageFolder", () => {
  it("finds the package of a file in node_modules", () => {
    expect(packageFolder("D:/git/app/frontend/node_modules/react/cjs/react.production.js", root, project, isPackage)).toBe(
      "node_modules/react",
    );
    expect(
      packageFolder("D:/git/app/frontend/node_modules/@fluentui/react-button/lib/Button.js", root, project, isPackage),
    ).toBe("node_modules/@fluentui/react-button");
  });

  it("takes the innermost package of a nested one", () => {
    expect(
      packageFolder("D:/git/app/frontend/node_modules/tabster/node_modules/keyborg/dist/index.js", root, project, isPackage),
    ).toBe("node_modules/tabster/node_modules/keyborg");
  });

  it("reads Windows paths, another drive letter case and query strings", () => {
    expect(packageFolder("d:\\git\\app\\frontend\\node_modules\\zustand\\index.js?commonjs-es-import", root, project, isPackage)).toBe(
      "node_modules/zustand",
    );
  });

  it("names the package of code the bundler adds itself", () => {
    expect(packageFolder("\0rolldown/runtime.js", root, project, isPackage)).toBe("node_modules/rolldown");
    expect(packageFolder("\0@scope/helpers/x.js", root, project, isPackage)).toBe("node_modules/@scope/helpers");
  });

  it("cannot tell a virtual module whose package is not installed", () => {
    expect(packageFolder("\0vite/modulepreload-polyfill.js", root, project, isPackage)).toBeUndefined();
    expect(packageFolder("\0commonjsHelpers.js", root, project, isPackage)).toBeUndefined();
  });

  it("counts the repository's own files as the project's", () => {
    expect(packageFolder("D:/git/app/frontend/src/main.tsx", root, project, isPackage)).toBe("");
    expect(packageFolder("D:/git/app/frontend/bindings/github.com/x/api/index.ts", root, project, isPackage)).toBe("");
    expect(packageFolder("D:/git/app/build/icon/appicon-32.svg?no-inline", root, project, isPackage)).toBe("");
  });

  it("cannot tell a file outside the repository", () => {
    expect(packageFolder("C:/elsewhere/lib.js", root, project, isPackage)).toBeUndefined();
    expect(packageFolder("D:/git/application/src/x.ts", root, project, isPackage)).toBeUndefined();
    expect(packageFolder("D:/other/node_modules/react/index.js", root, project, isPackage)).toBeUndefined();
  });
});
