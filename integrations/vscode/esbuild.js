// Build script for the DL Prism VS Code extension.
// Bundles the extension host entry point with esbuild. No runtime
// dependencies are produced or expected — everything the extension needs
// ships in dist/: the bundled extension.js plus the plain webview assets
// (copied verbatim from src/webview/, since they run in the webview's
// browser context and are never bundled by esbuild).
const esbuild = require("esbuild");
const fs = require("fs");
const path = require("path");

const watch = process.argv.includes("--watch");
const production = process.argv.includes("--production");

function copyWebviewAssets() {
  const srcDir = path.join(__dirname, "src", "webview");
  const outDir = path.join(__dirname, "dist", "webview");
  fs.mkdirSync(outDir, { recursive: true });
  for (const file of fs.readdirSync(srcDir)) {
    fs.copyFileSync(path.join(srcDir, file), path.join(outDir, file));
  }
  console.log(`[esbuild] copied webview assets -> dist/webview/`);
}

async function main() {
  copyWebviewAssets();

  const ctx = await esbuild.context({
    entryPoints: ["src/extension.ts"],
    bundle: true,
    outfile: "dist/extension.js",
    platform: "node",
    target: "node18",
    format: "cjs",
    external: ["vscode"],
    sourcemap: !production,
    minify: production,
    logLevel: "info",
  });

  if (watch) {
    await ctx.watch();
    console.log("[esbuild] watching for changes...");
  } else {
    await ctx.rebuild();
    await ctx.dispose();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
