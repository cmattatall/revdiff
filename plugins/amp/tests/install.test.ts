import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { copyFile, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

test('installer works outside the checkout, preserves other plugins, and requires explicit replacement', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'revdiff-install-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const source = join(root, 'source checkout')
  const home = join(root, 'user home')
  const destination = join(home, '.config/amp/plugins')
  await mkdir(source)
  await mkdir(home)
  await copyFile(new URL('../install.sh', import.meta.url), join(source, 'install.sh'))
  await copyFile(new URL('../revdiff.ts', import.meta.url), join(source, 'revdiff.ts'))
  const expected = await readFile(join(source, 'revdiff.ts'), 'utf8')
  const run = (...args: string[]) => spawnSync('/bin/bash', [join(source, 'install.sh'), ...args], {
    cwd: home, env: { ...process.env, HOME: home }, encoding: 'utf8',
  })

  const installed = run()
  assert.equal(installed.status, 0, installed.stderr)
  assert.match(installed.stdout, /Reload Amp plugins/)
  assert.equal(await readFile(join(destination, 'revdiff.ts'), 'utf8'), expected)
  assert.deepEqual(await readdir(destination), ['revdiff.ts'], 'only the plugin is installed')
  assert.equal(run().status, 0, 'reinstalling the same version is allowed')

  await writeFile(join(destination, 'unrelated.ts'), 'keep this plugin')
  await writeFile(join(destination, 'revdiff.ts'), 'local customizations')
  const refused = run()
  assert.equal(refused.status, 1)
  assert.match(refused.stderr, /--force/)
  assert.equal(await readFile(join(destination, 'revdiff.ts'), 'utf8'), 'local customizations')
  assert.equal(run('--force').status, 0)
  assert.equal(await readFile(join(destination, 'revdiff.ts'), 'utf8'), expected)
  assert.equal(await readFile(join(destination, 'unrelated.ts'), 'utf8'), 'keep this plugin')
  assert.equal(run('--typo').status, 2)
  assert.equal(run('--force', 'extra').status, 2)

  await rm(join(destination, 'revdiff.ts'))
  await symlink(join(destination, 'unrelated.ts'), join(destination, 'revdiff.ts'))
  assert.equal(run('--force').status, 1, 'even force must not write through a symlink')
  assert.equal(await readFile(join(destination, 'unrelated.ts'), 'utf8'), 'keep this plugin')
  await rm(join(destination, 'revdiff.ts'))
  await mkdir(join(destination, 'revdiff.ts'))
  assert.equal(run('--force').status, 1, 'must not copy into a directory named revdiff.ts')
  assert.deepEqual(await readdir(join(destination, 'revdiff.ts')), [])
})
