import { defineConfig, searchForWorkspaceRoot } from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";

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
  plugins: [react(), wails("./bindings")],
});
