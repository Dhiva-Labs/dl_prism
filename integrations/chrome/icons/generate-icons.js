#!/usr/bin/env node
// generate-icons.js — draws the DL Prism toolbar icon (a triangle prism
// splitting a beam into color) at 16/32/48/128px and writes raw PNGs.
//
// No npm dependencies: pixels are computed by hand and encoded into PNG
// chunks directly, using only Node's built-in `zlib` module for the
// DEFLATE/zlib compression that the PNG IDAT chunk requires (RFC 1950 —
// exactly what zlib.deflateSync produces). This script is a dev-time tool;
// nothing it imports ships inside the extension itself.

import { deflateSync } from 'node:zlib';
import { writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const __dirname = dirname(fileURLToPath(import.meta.url));

const SIZES = [16, 32, 48, 128];

// ---- color helpers --------------------------------------------------

function lerp(a, b, t) {
  return a + (b - a) * t;
}

function mixColor(c1, c2, t) {
  return [lerp(c1[0], c2[0], t), lerp(c1[1], c2[1], t), lerp(c1[2], c2[2], t)];
}

const BG_TOP = [109, 91, 240]; // #6d5bf0
const BG_BOTTOM = [139, 123, 255]; // #8b7bff
const WHITE = [255, 255, 255];

// ---- geometry helpers -------------------------------------------------

function roundedRectAlpha(x, y, size, radius) {
  // Returns 1 inside the rounded square, 0 outside, with a 1px soft edge.
  const cx = Math.min(Math.max(x, radius), size - radius);
  const cy = Math.min(Math.max(y, radius), size - radius);
  const dx = x - cx;
  const dy = y - cy;
  const dist = Math.sqrt(dx * dx + dy * dy);
  if (x >= radius && x <= size - radius) return 1;
  if (y >= radius && y <= size - radius) return 1;
  return dist <= radius ? 1 : dist <= radius + 1 ? radius + 1 - dist : 0;
}

function sign(px, py, ax, ay, bx, by) {
  return (px - bx) * (ay - by) - (ax - bx) * (py - by);
}

function triangleAlpha(x, y, ax, ay, bx, by, cx, cy) {
  const d1 = sign(x, y, ax, ay, bx, by);
  const d2 = sign(x, y, bx, by, cx, cy);
  const d3 = sign(x, y, cx, cy, ax, ay);
  const hasNeg = d1 < 0 || d2 < 0 || d3 < 0;
  const hasPos = d1 > 0 || d2 > 0 || d3 > 0;
  return hasNeg && hasPos ? 0 : 1;
}

function circleAlpha(x, y, cx, cy, r) {
  const d = Math.hypot(x - cx, y - cy);
  if (d <= r - 0.5) return 1;
  if (d <= r + 0.5) return r + 0.5 - d;
  return 0;
}

// ---- pixel drawing ------------------------------------------------------

function drawIcon(size) {
  const pixels = new Uint8ClampedArray(size * size * 4);
  const radius = Math.max(2, Math.round(size * 0.2));

  // Prism triangle, centered-ish, pointing up.
  const ax = size * 0.5;
  const ay = size * 0.2;
  const bx = size * 0.24;
  const by = size * 0.76;
  const cx = size * 0.76;
  const cy = size * 0.76;

  // Dispersed-light dots on the right, only rendered at larger sizes where
  // they'd read as more than noise.
  const showDots = size >= 32;
  const dotColors = [
    [255, 99, 99], // red
    [255, 205, 86], // yellow
    [99, 209, 130], // green
    [99, 165, 255], // blue
  ];

  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const idx = (y * size + x) * 4;

      const bgAlpha = roundedRectAlpha(x + 0.5, y + 0.5, size, radius);
      if (bgAlpha <= 0) {
        pixels[idx] = 0;
        pixels[idx + 1] = 0;
        pixels[idx + 2] = 0;
        pixels[idx + 3] = 0;
        continue;
      }

      const t = (x + y) / (2 * size);
      let [r, g, b] = mixColor(BG_TOP, BG_BOTTOM, t);
      let a = bgAlpha;

      const triAlpha = triangleAlpha(x + 0.5, y + 0.5, ax, ay, bx, by, cx, cy);
      if (triAlpha > 0) {
        [r, g, b] = WHITE;
      }

      if (showDots) {
        const dotY = size * 0.5;
        const spacing = size * 0.075;
        const startX = size * 0.82;
        const radiusDot = Math.max(1, size * 0.035);
        for (let i = 0; i < dotColors.length; i++) {
          const dAlpha = circleAlpha(x + 0.5, y + 0.5, startX, dotY + (i - 1.5) * spacing, radiusDot);
          if (dAlpha > 0) {
            [r, g, b] = mixColor([r, g, b], dotColors[i], dAlpha);
          }
        }
      }

      pixels[idx] = r;
      pixels[idx + 1] = g;
      pixels[idx + 2] = b;
      pixels[idx + 3] = Math.round(a * 255);
    }
  }

  return pixels;
}

// ---- minimal PNG encoder -------------------------------------------------

const CRC_TABLE = (() => {
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) {
      c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    }
    table[n] = c >>> 0;
  }
  return table;
})();

function crc32(buf) {
  let c = 0xffffffff;
  for (let i = 0; i < buf.length; i++) {
    c = CRC_TABLE[(c ^ buf[i]) & 0xff] ^ (c >>> 8);
  }
  return (c ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const typeBuf = Buffer.from(type, 'ascii');
  const lenBuf = Buffer.alloc(4);
  lenBuf.writeUInt32BE(data.length, 0);
  const crcBuf = Buffer.alloc(4);
  crcBuf.writeUInt32BE(crc32(Buffer.concat([typeBuf, data])), 0);
  return Buffer.concat([lenBuf, typeBuf, data, crcBuf]);
}

function encodePNG(size, pixels) {
  const signature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);

  const ihdrData = Buffer.alloc(13);
  ihdrData.writeUInt32BE(size, 0);
  ihdrData.writeUInt32BE(size, 4);
  ihdrData.writeUInt8(8, 8); // bit depth
  ihdrData.writeUInt8(6, 9); // color type 6 = RGBA
  ihdrData.writeUInt8(0, 10); // compression
  ihdrData.writeUInt8(0, 11); // filter
  ihdrData.writeUInt8(0, 12); // interlace
  const ihdr = chunk('IHDR', ihdrData);

  // Raw scanlines: each row prefixed with filter-type byte 0 (None).
  const rowBytes = size * 4;
  const raw = Buffer.alloc((rowBytes + 1) * size);
  for (let y = 0; y < size; y++) {
    const rowStart = y * (rowBytes + 1);
    raw[rowStart] = 0; // filter: none
    Buffer.from(pixels.buffer, y * rowBytes, rowBytes).copy(raw, rowStart + 1);
  }
  const idat = chunk('IDAT', deflateSync(raw));

  const iend = chunk('IEND', Buffer.alloc(0));

  return Buffer.concat([signature, ihdr, idat, iend]);
}

// ---- main ---------------------------------------------------------------

for (const size of SIZES) {
  const pixels = drawIcon(size);
  const png = encodePNG(size, pixels);
  const outPath = join(__dirname, `icon${size}.png`);
  writeFileSync(outPath, png);
  console.log(`wrote ${outPath} (${png.length} bytes)`);
}
