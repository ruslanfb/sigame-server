// Vite's emptyOutDir wipes dist/; the Go server embeds web/dist and needs the
// directory to exist in a fresh checkout, so we keep a .gitkeep in it.
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = join(dirname(fileURLToPath(import.meta.url)), '..', 'dist');
mkdirSync(dist, { recursive: true });
writeFileSync(join(dist, '.gitkeep'), '');
