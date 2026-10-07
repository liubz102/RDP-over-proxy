import { defineConfig, searchForWorkspaceRoot } from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";
import { bundledPackages } from "./plugins/bundledPackages";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
    fs: {
      // The app icon's drawings live with the build files (../build/icon),
      // the one place they are kept; the sidebar shows them too.
      allow: [searchForWorkspaceRoot(process.cwd()), "../build/icon"],
    },
  },
  build: {
    // The window is WebView2, which preloads modules itself; the polyfill
    // would only add Vite's code to the bundle.
    modulePreload: { polyfill: false },
  },
  // bundledPackages tells the third-party notices which packages the
  // bundle holds.
  plugins: [react(), wails("./bindings"), bundledPackages()],
});
