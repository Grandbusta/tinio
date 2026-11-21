import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin } from "vite";

const at = (p: string) => fileURLToPath(new URL(p, import.meta.url));

// The screens are the web app's, so its imports resolve here: @/ is src, @tinio/* the
// copied packages, and next/link and next/navigation small shims over the router.
// Under wails dev, Wails asks this server first and hands what it hasn't got (a 404) to
// the app; without this, /api would get the page, as any address of the app's does.
const apiIsTheApps: Plugin = {
  name: "api-is-the-apps",
  configureServer(server) {
    server.middlewares.use((req, res, next) => {
      if (process.env.TINIO_API || !req.url?.startsWith("/api/")) return next();
      res.statusCode = 404;
      res.end();
    });
  },
};

export default defineConfig({
  plugins: [react(), tailwindcss(), apiIsTheApps],
  resolve: {
    alias: {
      "@/": at("./src/"),
      "@tinio/ui": at("./src/packages/ui/index.ts"),
      "@tinio/brand": at("./src/packages/brand/index.ts"),
      "@tinio/tokens/theme.css": at("./src/packages/tokens/theme.css"),
      "@tinio/tokens": at("./src/packages/tokens/index.ts"),
      "next/link": at("./src/shims/link.tsx"),
      "next/navigation": at("./src/shims/navigation.ts"),
    },
  },
  // Worked on in a plain browser (TINIO_API=http://127.0.0.1:8081 npm run dev), /api is
  // a tinio command running there, as the window's /api is the API inside. Under wails
  // dev there's no TINIO_API: the app answers /api itself.
  server: {
    port: 5173,
    strictPort: true,
    proxy: process.env.TINIO_API
      ? { "/api": { target: process.env.TINIO_API, rewrite: (p) => p.replace(/^\/api\//, "/v1/") } }
      : undefined,
  },
  build: { outDir: "dist", emptyOutDir: true, chunkSizeWarningLimit: 1500 }, // read from the app, never a network
});
