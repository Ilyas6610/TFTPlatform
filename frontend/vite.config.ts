import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // API_URL points the dev server at another backend (e.g. a second
      // checkout running on a different port).
      "/api": process.env.API_URL ?? "http://localhost:8080",
    },
  },
});
