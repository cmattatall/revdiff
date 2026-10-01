import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { chmod, copyFile, lstat, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

type Fixture = Awaited<ReturnType<typeof fixture>>

async function fixture(t: Parameters<Parameters<typeof test>[1]>[0]) {
  const root = await mkdtemp(join(tmpdir(), 'revdiff-install-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const source = join(root, 'source checkout')
  const home = join(root, 'user home')
  const pluginSource = join(source, 'plugins/amp')
  const fakeBin = join(root, 'fake bin')
  await mkdir(pluginSource, { recursive: true })
  await mkdir(join(source, 'app/revdiff'), { recursive: true })
  await mkdir(home)
  await mkdir(fakeBin)
  await copyFile(new URL('../install.sh', import.meta.url), join(pluginSource, 'install.sh'))
  await copyFile(new URL('../revdiff.ts', import.meta.url), join(pluginSource, 'revdiff.ts'))
  const fakeGo = join(fakeBin, 'go')
  await writeFile(fakeGo, `#!/bin/sh
set -eu
printf '%s\\n' "$PWD" > "$FAKE_GO_LOG"
printf '%s\\n' "$@" >> "$FAKE_GO_LOG"
[ "\${FAKE_GO_FAIL:-0}" != 1 ] || exit 23
out=
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then out=$2; shift; fi
  shift
done
printf '%s' "\${FAKE_GO_CONTENT:-fake revdiff}" > "$out"
`)
  await chmod(fakeGo, 0o755)
  const destination = join(home, '.config/amp/plugins')
  const binary = join(home, '.local/bin/revdiff')
  const expected = await readFile(join(pluginSource, 'revdiff.ts'), 'utf8')
  const run = (args: string[] = [], extraEnv: Record<string, string | undefined> = {}) => {
    const unsetShell = Object.prototype.hasOwnProperty.call(extraEnv, 'SHELL') && extraEnv.SHELL === undefined
    const env: NodeJS.ProcessEnv = {
      ...process.env,
      HOME: home,
      PATH: `${fakeBin}:${process.env.PATH ?? '/usr/bin:/bin'}`,
      SHELL: '/bin/bash',
      FAKE_GO_LOG: join(root, 'go.log'),
      ...extraEnv,
    }
    for (const [key, value] of Object.entries(env)) if (value === undefined) delete env[key]
    const command = unsetShell
      ? ['-c', 'script=$1; shift; unset SHELL; source "$script" "$@"', 'installer', join(pluginSource, 'install.sh'), ...args]
      : [join(pluginSource, 'install.sh'), ...args]
    return spawnSync('/bin/bash', command, {
      cwd: home, env, encoding: 'utf8',
    })
  }
  return { root, source, home, destination, binary, expected, run }
}

function ok(result: ReturnType<Fixture['run']>) {
  assert.equal(result.status, 0, result.stderr)
}

const marker = '# revdiff: user-local binaries'

test('builds from the resolved repo and installs safely in paths with spaces', async (t) => {
  const f = await fixture(t)

  const installed = f.run()
  ok(installed)
  assert.match(installed.stdout, /Reload Amp plugins/)
  assert.equal(await readFile(join(f.destination, 'revdiff.ts'), 'utf8'), f.expected)
  assert.equal(await readFile(f.binary, 'utf8'), 'fake revdiff')
  assert.deepEqual(await readdir(f.destination), ['revdiff.ts'])
  assert.equal((await lstat(join(f.destination, 'revdiff.ts'))).mode & 0o777, 0o644)
  assert.equal((await lstat(f.binary)).mode & 0o777, 0o755)
  assert.equal((await readFile(join(f.root, 'go.log'), 'utf8')).split('\n')[0], f.source)
  assert.match(await readFile(join(f.root, 'go.log'), 'utf8'), /\.\/app\/revdiff/)
})

test('auto-detects bash and chooses the first existing login profile', async (t) => {
  for (const chosen of ['.bash_profile', '.bash_login', '.profile']) {
    await t.test(chosen, async (t) => {
      const f = await fixture(t)
      if (chosen !== '.bash_profile') await writeFile(join(f.home, chosen), 'existing\n')
      ok(f.run([], { SHELL: '/usr/local/bin/bash' }))
      assert.match(await readFile(join(f.home, '.bashrc'), 'utf8'), new RegExp(marker))
      assert.match(await readFile(join(f.home, chosen), 'utf8'), new RegExp(marker))
      if (chosen !== '.bash_profile') await assert.rejects(readFile(join(f.home, '.bash_profile')))
    })
  }
})

test('auto-detects zsh, honors ZDOTDIR, and supports an explicit override', async (t) => {
  const f = await fixture(t)
  const zdot = join(f.home, 'zsh config')
  ok(f.run([], { SHELL: '/bin/zsh', ZDOTDIR: zdot }))
  assert.match(await readFile(join(zdot, '.zshrc'), 'utf8'), new RegExp(marker))
  const override = await fixture(t)
  ok(override.run(['--shell', 'zsh'], { SHELL: '/bin/fish' }))
  assert.match(await readFile(join(override.home, '.zshrc'), 'utf8'), new RegExp(marker))
  const help = override.run(['--help'])
  ok(help)
  assert.match(help.stdout, /overrides automatic shell detection from \$SHELL/)
})

test('no-path, unknown shell, and unset SHELL do not modify startup files', async (t) => {
  const noPath = await fixture(t)
  ok(noPath.run(['--no-path']))
  assert.deepEqual((await readdir(noPath.home)).sort(), ['.config', '.local'])

  for (const shell of ['/bin/fish', undefined]) {
    const f = await fixture(t)
    const result = f.run([], { SHELL: shell })
    ok(result)
    assert.match(result.stderr, /not supported.*configure PATH manually/)
    assert.deepEqual((await readdir(f.home)).sort(), ['.config', '.local'])
  }
})

test('reinstall is idempotent and replaces different regular files by default', async (t) => {
  const f = await fixture(t)
  ok(f.run())
  ok(f.run())
  const bashrc = await readFile(join(f.home, '.bashrc'), 'utf8')
  assert.equal(bashrc.split(marker).length - 1, 1)

  await writeFile(join(f.destination, 'unrelated.ts'), 'keep this plugin')
  await writeFile(join(f.destination, 'revdiff.ts'), 'local customizations')
  ok(f.run())
  assert.equal(await readFile(join(f.destination, 'revdiff.ts'), 'utf8'), f.expected)
  assert.equal(await readFile(join(f.destination, 'unrelated.ts'), 'utf8'), 'keep this plugin')

  ok(f.run([], { FAKE_GO_CONTENT: 'new binary' }))
  assert.equal(await readFile(f.binary, 'utf8'), 'new binary')
  assert.doesNotMatch(f.run(['--help']).stdout, /--force/)
  assert.equal(f.run(['--force']).status, 2)
  assert.equal(f.run(['--typo']).status, 2)
  assert.equal(f.run(['--shell']).status, 2)
  assert.equal(f.run(['--shell', 'fish']).status, 2)
})

test('refuses plugin and binary symlinks', async (t) => {
  const plugin = await fixture(t)
  await mkdir(plugin.destination, { recursive: true })
  const unrelated = join(plugin.destination, 'unrelated.ts')
  await writeFile(unrelated, 'keep this plugin')
  await symlink(unrelated, join(plugin.destination, 'revdiff.ts'))
  assert.equal(plugin.run().status, 1)
  assert.equal(await readFile(unrelated, 'utf8'), 'keep this plugin')

  const binary = await fixture(t)
  await mkdir(join(binary.home, '.local/bin'), { recursive: true })
  const target = join(binary.root, 'important')
  await writeFile(target, 'do not replace')
  await symlink(target, binary.binary)
  assert.equal(binary.run().status, 1)
  assert.equal(await readFile(target, 'utf8'), 'do not replace')
})

test('a failed build preserves installed files and leaves no temporary files', async (t) => {
  const f = await fixture(t)
  ok(f.run())
  await writeFile(join(f.destination, 'revdiff.ts'), 'installed plugin')
  await writeFile(f.binary, 'installed binary')
  const failed = f.run([], { FAKE_GO_FAIL: '1' })
  assert.equal(failed.status, 23)
  assert.equal(await readFile(join(f.destination, 'revdiff.ts'), 'utf8'), 'installed plugin')
  assert.equal(await readFile(f.binary, 'utf8'), 'installed binary')
  assert.deepEqual(await readdir(f.destination), ['revdiff.ts'])
  assert.deepEqual(await readdir(join(f.home, '.local/bin')), ['revdiff'])
})
