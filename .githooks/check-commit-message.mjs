import { execFileSync, spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { normalizeCommitMessage } from './commitlint.config.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);

try {
  if (args.length > 1) throw new Error('Usage: npm run lint:commit -- <message-file>');
  const path = args[0] ?? execFileSync('git', ['rev-parse', '--git-path', 'COMMIT_EDITMSG'], {
    cwd: root, encoding: 'utf8', windowsHide: true,
  }).trim();
  const message = normalizeCommitMessage(readFileSync(resolve(root, path), 'utf8'));
  // commitlint skips empty files and parses comments before some raw-line rules.
  if (!message) throw new Error('Write a commit title and prose body; comments alone do not count.');

  const manifestPath = join(root, 'node_modules', '@commitlint', 'cli', 'package.json');
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  const result = spawnSync(process.execPath, [
    resolve(dirname(manifestPath), manifest.bin.commitlint), '--config', join(root, '.githooks', 'commitlint.config.mjs'),
  ], {
    cwd: root,
    input: message,
    stdio: ['pipe', 'inherit', 'inherit'],
    windowsHide: true,
  });
  if (result.error) throw result.error;
  process.exitCode = result.status ?? 1;
} catch (error) {
  console.error(`Commit validation failed: ${error.message}`);
  process.exitCode = 1;
}
