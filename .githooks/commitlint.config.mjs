import { execFileSync } from 'node:child_process';

const commitTypes = Object.freeze([
  'feat', 'fix', 'docs', 'style', 'refactor', 'test',
  'chore', 'perf', 'build', 'revert', 'ci', 'deps',
]);

const commitHeaderPattern = /^([a-z]+)\(([^()\r\n]+)\): (\S[^\r\n]*)$/;

function isCommitHeader(header) {
  if (typeof header !== 'string' || header !== header.trim()) return false;
  const match = commitHeaderPattern.exec(header);
  return Boolean(match && commitTypes.includes(match[1])
    && match[2].trim() && match[2] === match[2].trim());
}

function gitOutput(args, input) {
  return execFileSync('git', args, { input, encoding: 'utf8', windowsHide: true });
}

function isTrailerBlock(block) {
  let hasTrailer = false;
  for (const line of block.split('\n')) {
    if (/^[A-Za-z][A-Za-z0-9 -]*:[ \t]*.*$/.test(line)) {
      hasTrailer = true;
    } else if (!hasTrailer || !/^[ \t]+\S/.test(line)) {
      return false;
    }
  }
  return hasTrailer;
}

export function normalizeCommitMessage(raw) {
  const normalized = raw.replace(/^\uFEFF/u, '').replace(/\r\n?/g, '\n');
  // Git's verbose commit template puts a diff below this cleanup boundary.
  const beforeDiff = normalized.split(/^# ------------------------ >8 ------------------------$/m)[0];
  return gitOutput(['stripspace', '--strip-comments'], beforeDiff).trimEnd();
}

function inspectCommitMessage(raw) {
  let remaining = normalizeCommitMessage(raw);
  const header = remaining.split('\n')[0] ?? '';
  const trailers = [];

  // Ask Git which final paragraphs are trailers instead of counting them as prose.
  while (remaining) {
    const parsed = gitOutput(['interpret-trailers', '--parse'], remaining).trim();
    const boundary = remaining.lastIndexOf('\n\n');
    if (!parsed || boundary < 0 || !isTrailerBlock(remaining.slice(boundary + 2))) break;
    trailers.unshift(...parsed.split('\n'));
    remaining = remaining.slice(0, boundary).trimEnd();
  }

  const body = remaining.slice(remaining.indexOf('\n') < 0
    ? remaining.length : remaining.indexOf('\n') + 1).trim();
  const signOffs = trailers.filter((line) => line.startsWith('Signed-off-by:'));
  const hasSignOff = signOffs.length > 0 && signOffs.every((line) => {
    const match = /^Signed-off-by: ([^<>]+) <([^<>\s@]+@[^<>\s@]+)>$/u.exec(line);
    return Boolean(match && match[1].trim() && match[1] === match[1].trim());
  });
  return { header, body, hasSignOff };
}

export default {
  defaultIgnores: false,
  parserPreset: {
    parserOpts: {
      headerPattern: commitHeaderPattern,
      headerCorrespondence: ['type', 'scope', 'subject'],
      commentChar: '#',
    },
  },
  plugins: [{
    rules: {
      'header-format': ({ header }) => [
        isCommitHeader(header),
        'Use type(scope): description with a nonblank, trimmed scope.',
      ],
      'body-and-sign-off': ({ raw }) => {
        const { body, hasSignOff } = inspectCommitMessage(raw);
        if (!body) return [false, 'Add a prose body; comments and trailers do not count.'];
        return [hasSignOff, 'End with Signed-off-by: Name <email@example.com>.'];
      },
    },
  }],
  rules: {
    'type-empty': [2, 'never'],
    'type-case': [2, 'always', 'lower-case'],
    'type-enum': [2, 'always', commitTypes],
    'scope-empty': [2, 'never'],
    'subject-empty': [2, 'never'],
    'header-trim': [2, 'always'],
    'body-leading-blank': [2, 'always'],
    'trailer-exists': [2, 'always', 'Signed-off-by:'],
    'header-format': [2, 'always'],
    'body-and-sign-off': [2, 'always'],
  },
};
