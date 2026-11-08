#!/usr/bin/env node
// tool/package.mjs — build, then zip dist/firefox for addons.mozilla.org.
//
// Usage: node tool/package.mjs
// Writes dist/dl-prism-companion-firefox-<version>.zip with manifest.json at
// the root of the zip. Needs Info-ZIP `zip`.
//
// No source archive is needed: the package holds the repository's own files,
// copied unchanged; tool/build.mjs only writes a Firefox-flavoured manifest.

import { execFileSync } from 'node:child_process';
import { readFileSync, rmSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const { version } = JSON.parse(readFileSync(join(ROOT, 'manifest.json'), 'utf8'));
const zip = join(ROOT, 'dist', `dl-prism-companion-firefox-${version}.zip`);

execFileSync(process.execPath, [join(ROOT, 'tool', 'build.mjs')], { stdio: 'inherit' });
rmSync(zip, { force: true });
execFileSync('zip', ['-qrX', zip, '.'], { cwd: join(ROOT, 'dist', 'firefox'), stdio: 'inherit' });
console.log(`packaged ${zip}`);
