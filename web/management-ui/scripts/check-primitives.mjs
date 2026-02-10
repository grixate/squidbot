import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../src/routes/', import.meta.url);
const disallowed = /<\s*(button|select)\b/gi;

function walk(dir, out = []) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) {
      walk(full, out);
      continue;
    }
    if (full.endsWith('.svelte')) {
      out.push(full);
    }
  }
  return out;
}

const files = walk(root.pathname);
const violations = [];
for (const file of files) {
  const content = readFileSync(file, 'utf8');
  let match;
  while ((match = disallowed.exec(content)) !== null) {
    const before = content.slice(0, match.index);
    const line = before.split('\n').length;
    violations.push(`${file}:${line}: ${match[0]}`);
  }
}

if (violations.length > 0) {
  console.error('Native <button>/<select> are not allowed in src/routes.');
  for (const item of violations) {
    console.error(item);
  }
  process.exit(1);
}

console.log('Primitive check passed.');
