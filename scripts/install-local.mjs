import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const platform=`${process.platform}-${process.arch==='x64'?'amd64':process.arch}`;
const archive=path.join(root,'dist',`termium-${platform}.tar.gz`);
const checksum=(await fs.readFile(archive+'.sha256','utf8')).split(/\s/)[0];
execFileSync('bash',[path.join(root,'scripts/install.sh'),'--archive',archive,'--checksum',checksum,'--no-launch'],{stdio:'inherit'});
