import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const dest = path.join(root, 'server/dist/extensions/vimium');
await fs.rm(dest, { recursive: true, force: true });
await fs.mkdir(path.dirname(dest), { recursive: true });
await fs.cp(path.join(root, 'third_party/vimium'), dest, { recursive: true });
