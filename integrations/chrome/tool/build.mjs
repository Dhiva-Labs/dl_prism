#!/usr/bin/env node
// tool/build.mjs — zero-dependency build for the two loadable targets.
//
// The repo root stays a plain, zero-build-step Chrome extension (manifest.json
// here is Chrome-flavored and loads unpacked as-is, unchanged). This script
// copies the shared sources — background.js, common/, popup/, options/,
// icons/ — into dist/chrome and dist/firefox, writing each a manifest.json
// tailored to that browser's MV3 quirks:
//
//   dist/chrome/   — manifest.json copied verbatim from the repo root
//                     (background.service_worker).
//   dist/firefox/  — background.scripts (event page) instead of
//                     service_worker (Firefox does not reliably support MV3
//                     service workers), plus browser_specific_settings.gecko
//                     so `web-ext` and about:debugging can identify it.
//
// dist/chrome exists mainly so both targets can be built and verified the
// same way; loading the repo root directly in Chrome still works exactly as
// before.
//
// Usage: node tool/build.mjs

import { readFileSync, writeFileSync, mkdirSync, rmSync, cpSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join, relative, sep } from 'node:path';

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, '..');
const DIST = join(ROOT, 'dist');

// Everything a loadable target needs, relative to ROOT. manifest.json is
// handled separately per target (see below) — it is not copied verbatim.
const SHARED_ENTRIES = ['background.js', 'common', 'popup', 'options', 'icons'];

// Dev-only files that live inside one of the shared dirs above but must not
// ship in either target.
const EXCLUDE = new Set(['icons/generate-icons.js']);

function copyShared(targetDir) {
  for (const entry of SHARED_ENTRIES) {
    const src = join(ROOT, entry);
    const dest = join(targetDir, entry);
    cpSync(src, dest, {
      recursive: true,
      filter: (source) => {
        const rel = relative(ROOT, source).split(sep).join('/');
        return !EXCLUDE.has(rel);
      },
    });
  }
}

function writeManifest(targetDir, manifest) {
  writeFileSync(join(targetDir, 'manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`);
}

function buildChrome(baseManifest) {
  const dir = join(DIST, 'chrome');
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });
  copyShared(dir);
  // The source manifest.json is already Chrome-flavored — ship it as-is so
  // there is exactly one place background/permissions/icons are defined for
  // Chrome.
  writeManifest(dir, baseManifest);
  console.log(`built ${relative(ROOT, dir)}/`);
}

function buildFirefox(baseManifest) {
  const dir = join(DIST, 'firefox');
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });
  copyShared(dir);

  const manifest = JSON.parse(JSON.stringify(baseManifest)); // deep clone, no deps

  // Firefox does not reliably support MV3 service workers; use an event
  // page (background.scripts) instead. background.js is an ES module (it
  // uses `import`), so `type: module` has to travel with it — Firefox has
  // supported ES module background scripts since Firefox 89, well under our
  // strict_min_version floor below.
  manifest.background = { scripts: ['background.js'], type: 'module' };

  manifest.browser_specific_settings = {
    gecko: {
      id: 'dl-prism@dhivalabs.dev',
      strict_min_version: '121.0',
      // NOTE: `web-ext lint` warns MISSING_DATA_COLLECTION_PERMISSIONS
      // without a browser_specific_settings.gecko.data_collection_permissions
      // key (AMO policy — the extension collects none, only talking to the
      // user's own local gateway). That key requires strict_min_version
      // 140.0+ though, which conflicts with the 121.0 floor asked for here;
      // add `data_collection_permissions: { required: ['none'] }` once the
      // min-version floor is deliberately raised to 140+.
    },
  };

  writeManifest(dir, manifest);
  console.log(`built ${relative(ROOT, dir)}/`);
}

const baseManifest = JSON.parse(readFileSync(join(ROOT, 'manifest.json'), 'utf8'));

rmSync(DIST, { recursive: true, force: true });
mkdirSync(DIST, { recursive: true });
buildChrome(baseManifest);
buildFirefox(baseManifest);
