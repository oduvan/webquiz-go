// Small PNG pictures made in code, for quiz questions and image options.
//
// A picture is one shape (square, circle, triangle or stripes) in one colour on
// a light background. No image library is needed: the PNG is written by hand
// (one IHDR, one IDAT with zlib data, one IEND chunk).

import { deflateSync } from 'node:zlib';

export type Shape = 'square' | 'circle' | 'triangle' | 'stripes';

export interface PictureSpec {
  shape: Shape;
  /** Red, green, blue (0-255). */
  color: [number, number, number];
  size?: number;
}

const CRC_TABLE = (() => {
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    table[n] = c >>> 0;
  }
  return table;
})();

function crc32(data: Uint8Array): number {
  let c = 0xffffffff;
  for (const byte of data) c = CRC_TABLE[(c ^ byte) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function chunk(type: string, data: Buffer): Buffer {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const typed = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(typed));
  return Buffer.concat([length, typed, crc]);
}

/** Is the pixel (x, y) inside the shape? Coordinates are 0..size-1. */
function inside(shape: Shape, x: number, y: number, size: number): boolean {
  const margin = size * 0.15;
  const low = margin;
  const high = size - margin;
  switch (shape) {
    case 'square':
      return x >= low && x <= high && y >= low && y <= high;
    case 'circle': {
      const centre = size / 2;
      const radius = size / 2 - margin;
      return (x - centre) ** 2 + (y - centre) ** 2 <= radius ** 2;
    }
    case 'triangle': {
      if (y < low || y > high) return false;
      const half = ((y - low) / (high - low)) * ((high - low) / 2);
      return Math.abs(x - size / 2) <= half;
    }
    case 'stripes':
      return Math.floor(y / (size / 8)) % 2 === 0;
  }
}

/** A PNG file with one shape. */
export function makePicture(spec: PictureSpec): Buffer {
  const size = spec.size ?? 96;
  const [r, g, b] = spec.color;
  const rows: Buffer[] = [];
  for (let y = 0; y < size; y++) {
    const row = Buffer.alloc(1 + size * 3); // filter byte 0, then RGB
    for (let x = 0; x < size; x++) {
      const on = inside(spec.shape, x, y, size);
      row[1 + x * 3] = on ? r : 245;
      row[2 + x * 3] = on ? g : 245;
      row[3 + x * 3] = on ? b : 240;
    }
    rows.push(row);
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(size, 0);
  header.writeUInt32BE(size, 4);
  header[8] = 8; // bit depth
  header[9] = 2; // colour type: RGB
  header[10] = 0;
  header[11] = 0;
  header[12] = 0;
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', header),
    chunk('IDAT', deflateSync(Buffer.concat(rows))),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}
