/// <reference types="node" />
// Gzip copies of the built files. The Go server sends name.gz instead of name
// to browsers that accept gzip, so a student's page downloads far fewer bytes
// (internal/server/static.go, serveAssets).
import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { extname, join, resolve } from 'node:path';
import { constants, gzipSync } from 'node:zlib';
import type { Logger, Plugin } from 'vite';

// Text files that gzip makes much smaller. Pictures and fonts are packed already.
const COMPRESSIBLE = new Set(['.js', '.css', '.svg', '.json', '.html']);

// writeGzipCopies writes name.gz (best compression) next to every script,
// style, SVG, JSON and HTML file in dir and its folders. A file gets no copy
// when gzip does not make it smaller. It returns the paths of the copies.
export function writeGzipCopies(dir: string): string[] {
  const written: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      written.push(...writeGzipCopies(path));
    } else if (entry.isFile() && COMPRESSIBLE.has(extname(entry.name))) {
      const content = readFileSync(path);
      const packed = gzipSync(content, { level: constants.Z_BEST_COMPRESSION });
      if (packed.length < content.length) {
        writeFileSync(path + '.gz', packed);
        written.push(path + '.gz');
      }
    }
  }
  return written;
}

// gzipCopies is the Vite plugin that runs writeGzipCopies on the build folder
// after "vite build" has written all files. Vite empties that folder first,
// so no old copy stays behind.
export function gzipCopies(): Plugin {
  let outDir = '';
  let logger: Logger | undefined;
  return {
    name: 'webquiz-gzip-copies',
    apply: 'build',
    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir);
      logger = config.logger;
    },
    writeBundle() {
      const written = writeGzipCopies(outDir);
      logger?.info(`gzip copies: ${written.length} files`);
    },
  };
}
