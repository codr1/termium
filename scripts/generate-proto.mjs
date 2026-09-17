// Keep code generation independent of globally installed protoc/plugins.
// npm supplies protoc and ts-proto; Go generators are built once into a local cache.
import { spawnSync } from 'node:child_process';
import { mkdirSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const cache = path.join(root, 'node_modules/.cache/termium/protobuf');
const compiler = path.join(root, 'node_modules/protoc/protoc.cjs');
const plugins = [
  { name: 'protoc-gen-go', module: 'google.golang.org/protobuf/cmd/protoc-gen-go', version: 'v1.34.2' },
  { name: 'protoc-gen-go-grpc', module: 'google.golang.org/grpc/cmd/protoc-gen-go-grpc', version: 'v1.5.1' },
];

function run(command, args, env = process.env) {
  const result = spawnSync(command, args, { cwd: root, env, stdio: 'inherit' });
  if (result.error) throw result.error;
  if (result.status !== 0) throw Error(`${path.basename(command)} failed (${result.signal ?? result.status})`);
}

function ensurePlugins() {
  mkdirSync(cache, { recursive: true });
  for (const plugin of plugins) {
    const binary = path.join(cache, plugin.name);
    const existing = spawnSync(binary, ['--version'], { encoding: 'utf8' });
    const expected = plugin.version.replace(/^v/, '');
    if (existing.status === 0 && existing.stdout.trim().replace(/^.*?v?(\d+\.\d+\.\d+)$/, '$1') === expected) continue;
    console.log(`Preparing ${plugin.name} in the project build-tool cache…`);
    run('go', ['install', `${plugin.module}@${plugin.version}`], { ...process.env, GOBIN: cache });
  }
}

try {
  const args = process.argv.slice(2);
  if (args.length > 1 || (args.length && !['--go', '--ts', '--setup'].includes(args[0]))) {
    throw Error('Use --go, --ts, --setup, or no argument for both languages');
  }
  const mode = args[0];
  try { readFileSync(compiler); }
  catch { throw Error('The project protoc dependency is missing. Run npm ci, then retry.'); }
  mkdirSync(path.join(root, 'client/pb'), { recursive: true });
  mkdirSync(path.join(root, 'server/generated'), { recursive: true });
  if (mode !== '--ts') ensurePlugins();
  if (mode === '--setup') {
    run(process.execPath, [compiler, '--version']);
  } else {
    if (mode !== '--go') run(process.execPath, [compiler,
      `--plugin=protoc-gen-ts_proto=${path.join(root, 'node_modules/.bin/protoc-gen-ts_proto')}`,
      '--ts_proto_out=server/generated', '--ts_proto_opt=env=node,outputServices=grpc-js,useOptionals=messages',
      '--proto_path=proto', 'proto/bc.proto']);
    if (mode !== '--ts') run(process.execPath, [compiler,
      ...plugins.map(p => `--plugin=${p.name}=${path.join(cache, p.name)}`),
      '--go_out=client/pb', '--go_opt=paths=source_relative',
      '--go-grpc_out=client/pb', '--go-grpc_opt=paths=source_relative', '-I', 'proto', 'proto/bc.proto']);
  }
} catch (error) {
  console.error(`Protocol generation: ${error.message}`);
  process.exitCode = 1;
}
