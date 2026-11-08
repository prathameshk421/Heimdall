import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The Go binary embeds the two files this writes, so the running server needs no Node. Names stay fixed, and all code
// sits in one file, because the dashboard serves a short fixed list of assets.
export default defineConfig({
  plugins: [react()],
  define: { "process.env.NODE_ENV": JSON.stringify("production") },
  build: {
    outDir: "../../internal/transport/dashboard/assets",
    emptyOutDir: false,
    cssCodeSplit: false,
    modulePreload: false,
    rollupOptions: {
      input: "src/main.tsx",
      output: {
        format: "es",
        entryFileNames: "report.js",
        assetFileNames: "report.css",
        inlineDynamicImports: true,
      },
    },
  },
});
