#!/usr/bin/env node
// Generates PWA icon PNGs using only Node.js built-ins (no dependencies).
// Mirrors the stacked-bars design from public/favicon.svg.

const zlib = require('zlib');
const fs = require('fs');
const path = require('path');

const OUT_DIR = path.join(__dirname, '..', 'public', 'icons');

const BG = { r: 0x0a, g: 0x0a, b: 0x0b };
const ACCENT = { r: 0x22, g: 0xc5, b: 0x5e };

function blendPixel(buf, size, x, y, c, alpha) {
  if (x < 0 || x >= size || y < 0 || y >= size) return;
  const i = (y * size + x) * 4;
  const a = alpha;
  buf[i]     = Math.round(c.r * a + buf[i]     * (1 - a));
  buf[i + 1] = Math.round(c.g * a + buf[i + 1] * (1 - a));
  buf[i + 2] = Math.round(c.b * a + buf[i + 2] * (1 - a));
  buf[i + 3] = 255;
}

function fillSolid(buf, size, c) {
  for (let i = 0; i < size * size; i++) {
    buf[i * 4] = c.r; buf[i * 4 + 1] = c.g; buf[i * 4 + 2] = c.b; buf[i * 4 + 3] = 255;
  }
}

function fillRoundedRect(buf, size, x1, y1, x2, y2, radius, c, alpha = 1) {
  const ix1 = Math.round(x1), iy1 = Math.round(y1);
  const ix2 = Math.round(x2), iy2 = Math.round(y2);
  for (let y = iy1; y < iy2; y++) {
    for (let x = ix1; x < ix2; x++) {
      let inside = true;
      const corners = [
        [ix1 + radius, iy1 + radius, x < ix1 + radius && y < iy1 + radius],
        [ix2 - radius, iy1 + radius, x >= ix2 - radius && y < iy1 + radius],
        [ix1 + radius, iy2 - radius, x < ix1 + radius && y >= iy2 - radius],
        [ix2 - radius, iy2 - radius, x >= ix2 - radius && y >= iy2 - radius],
      ];
      for (const [cx, cy, active] of corners) {
        if (active) {
          const dx = x - cx, dy = y - cy;
          if (dx * dx + dy * dy > radius * radius) { inside = false; break; }
        }
      }
      if (inside) blendPixel(buf, size, x, y, c, alpha);
    }
  }
}

function maskRoundedBg(buf, size, radius) {
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const corners = [
        [radius, radius, x < radius && y < radius],
        [size - radius, radius, x >= size - radius && y < radius],
        [radius, size - radius, x < radius && y >= size - radius],
        [size - radius, size - radius, x >= size - radius && y >= size - radius],
      ];
      for (const [cx, cy, active] of corners) {
        if (active) {
          const dx = x - cx, dy = y - cy;
          if (dx * dx + dy * dy > radius * radius) {
            const i = (y * size + x) * 4;
            buf[i + 3] = 0;
          }
        }
      }
    }
  }
}

function createIcon(size) {
  const buf = Buffer.alloc(size * size * 4);
  const s = (v) => v * size / 64;

  fillSolid(buf, size, BG);

  // Bar 1: x=12, y=15, w=40, h=9, rx=4.5, alpha=1
  fillRoundedRect(buf, size, s(12), s(15), s(12 + 40), s(15 + 9), s(4.5), ACCENT, 1);
  // Bar 2: x=16, y=28, w=32, h=9, alpha=0.5
  fillRoundedRect(buf, size, s(16), s(28), s(16 + 32), s(28 + 9), s(4.5), ACCENT, 0.5);
  // Bar 3: x=20, y=41, w=24, h=9, alpha=0.25
  fillRoundedRect(buf, size, s(20), s(41), s(20 + 24), s(41 + 9), s(4.5), ACCENT, 0.25);

  maskRoundedBg(buf, size, Math.round(s(12)));

  return encodePNG(buf, size);
}

function encodePNG(pixels, size) {
  const rawLen = size * (size * 4 + 1);
  const raw = Buffer.alloc(rawLen);
  for (let y = 0; y < size; y++) {
    const rowOffset = y * (size * 4 + 1);
    raw[rowOffset] = 0;
    pixels.copy(raw, rowOffset + 1, y * size * 4, (y + 1) * size * 4);
  }
  const deflated = zlib.deflateSync(raw, { level: 9 });
  const chunks = [Buffer.from([137, 80, 78, 71, 13, 10, 26, 10])];
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; ihdr[9] = 6;
  chunks.push(makeChunk('IHDR', ihdr));
  chunks.push(makeChunk('IDAT', deflated));
  chunks.push(makeChunk('IEND', Buffer.alloc(0)));
  return Buffer.concat(chunks);
}

function makeChunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length, 0);
  const typeB = Buffer.from(type, 'ascii');
  const crcData = Buffer.concat([typeB, data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(crcData) >>> 0, 0);
  return Buffer.concat([len, typeB, data, crc]);
}

const crcTable = new Uint32Array(256);
for (let n = 0; n < 256; n++) {
  let c = n;
  for (let k = 0; k < 8; k++) c = (c & 1) ? (0xedb88320 ^ (c >>> 1)) : (c >>> 1);
  crcTable[n] = c;
}
function crc32(buf) {
  let c = 0xffffffff;
  for (let i = 0; i < buf.length; i++) c = crcTable[(c ^ buf[i]) & 0xff] ^ (c >>> 8);
  return c ^ 0xffffffff;
}

fs.mkdirSync(OUT_DIR, { recursive: true });
for (const [size, name] of [[180, 'apple-touch-icon.png'], [192, 'icon-192.png'], [512, 'icon-512.png']]) {
  fs.writeFileSync(path.join(OUT_DIR, name), createIcon(size));
  console.log(`${name} (${size}x${size})`);
}
console.log('Done!');
