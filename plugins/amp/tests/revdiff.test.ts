import assert from 'node:assert/strict'
import { access, readFile, stat } from 'node:fs/promises'
import { test } from 'node:test'
import { pathToFileURL } from 'node:url'

import plugin from '../revdiff.ts'

type Command = (ctx: any) => Promise<void>

function fakeAmp(root = process.cwd()) {
  const commands = new Map<string, Command>()
  const disposers: Array<() => Promise<void>> = []
  const messages = new Map<string, any[]>()
  const notifications: string[] = []
  const amp: any = {
    helpers: { filePathFromURI: (uri: URL) => uri.pathname },
    logger: { log() {} },
    registerCommand(id: string, _options: unknown, handler: Command) { commands.set(id, handler); return { unsubscribe() {} } },
    onDispose(handler: () => Promise<void>) { disposers.push(handler); return { unsubscribe() {} } },
  }
  const context = (id: string, append?: (...args: any[]) => Promise<void>) => ({
    thread: { id, appendUserMessage: async (...args: any[]) => {
      (messages.get(id) ?? messages.set(id, []).get(id)!).push(args)
      if (args[1]?.steer) await append?.(...args)
    } },
    system: { workspaceRoot: pathToFileURL(root) },
    ui: { notify: async (value: string) => { notifications.push(value) } },
  })
  return { amp, commands, disposers, messages, notifications, context }
}

async function connect(f: ReturnType<typeof fakeAmp>, id: string, append?: (...args: any[]) => Promise<void>) {
  await f.commands.get('revdiff-connect')!(f.context(id, append))
  const command = f.messages.get(id)!.at(-1)![0].content
  const path = command.match(/--amp '([^']+)'/)![1]
  return { path, descriptor: JSON.parse(await readFile(path, 'utf8')) }
}

async function post(descriptor: any, body: unknown, options: { token?: string; origin?: string; raw?: string } = {}) {
  return fetch(descriptor.url, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${options.token ?? descriptor.token}`,
      ...(options.origin ? { origin: options.origin } : {}),
    },
    body: options.raw ?? JSON.stringify(body),
  })
}

test('connect posts a persistent command to its thread, reuses connections, and cleans up', async () => {
  const f = fakeAmp(); await plugin(f.amp)
  await Promise.all([
    f.commands.get('revdiff-connect')!(f.context('T-a')),
    f.commands.get('revdiff-connect')!(f.context('T-a')),
  ])
  assert.equal(f.messages.get('T-a')!.length, 1, 'concurrent connects share one announcement')
  assert.deepEqual(f.notifications, [], 'the launch command must not be a transient popup')
  const [announcement, options] = f.messages.get('T-a')![0]
  assert.equal(announcement.type, 'user-message')
  assert.match(announcement.content, /```sh\n'revdiff' --amp '[^']+' --untracked\n```/)
  assert.match(announcement.content, /not a request for the agent to run commands/)
  assert.match(announcement.content, /\.\/\.bin\/revdiff/)
  assert.equal(options, undefined, 'setup is not steering feedback')
  const a = await connect(f, 'T-a'); const b = await connect(f, 'T-b')
  assert.notEqual(a.path, b.path); assert.notEqual(a.descriptor.token, b.descriptor.token)
  assert.deepEqual(Object.keys(a.descriptor), ['version', 'url', 'token', 'root', 'thread'])
  assert.equal(a.descriptor.thread, 'T-a'); assert.equal((await stat(a.path)).mode & 0o777, 0o600)
  const again = await connect(f, 'T-a'); assert.equal(again.path, a.path)
  assert.equal(f.messages.get('T-a')!.at(-1)![0].content, announcement.content)
  assert.ok(!announcement.content.includes(a.descriptor.token), 'never publish the authentication token')
  assert.ok(!f.messages.get('T-b')![0][0].content.includes(a.path), 'commands stay in their own threads')
  assert.deepEqual(f.notifications, [], 'repeated connect also avoids the popup')
  await f.commands.get('revdiff-disconnect')!(f.context('T-a'))
  await assert.rejects(access(a.path)); assert.equal((await fetch(a.descriptor.url).catch(() => null)), null)
  await Promise.all(f.disposers.map((dispose) => dispose()))
  await assert.rejects(access(b.path))
})

test('valid feedback steers with guidance and enforces endpoint, auth, origin, and input bounds', async () => {
  const f = fakeAmp(); await plugin(f.amp); const c = await connect(f, 'T-a')
  assert.equal((await post(c.descriptor, { id: 'one', content: 'fix this' })).status, 204)
  const [message, options] = f.messages.get('T-a')![1]
  assert.equal(options.steer, true); assert.match(message.content, /user owns staging/i); assert.match(message.content, /fix this/)
  assert.equal((await post(c.descriptor, {}, { token: 'bad' })).status, 401)
  assert.equal((await post(c.descriptor, {}, { origin: 'https://example.com' })).status, 403)
  assert.equal((await fetch(c.descriptor.url.replace('/feedback', '/other'), { method: 'POST' })).status, 404)
  assert.equal((await post(c.descriptor, { id: '', content: 'x' })).status, 400)
  assert.equal((await post(c.descriptor, { id: 'x'.repeat(257), content: 'x' })).status, 400)
  assert.equal((await post(c.descriptor, { id: 'x', content: ' ' })).status, 400)
  assert.equal((await post(c.descriptor, {}, { raw: '{' })).status, 400)
  assert.equal((await post(c.descriptor, {}, { raw: 'x'.repeat(1024 * 1024 + 1) })).status, 413)
  await Promise.all(f.disposers.map((dispose) => dispose()))
})

test('deduplicates concurrent feedback, detects conflicts, and caches ambiguous failures', async () => {
  let calls = 0; let release!: () => void
  const gate = new Promise<void>((resolve) => { release = resolve })
  const append = async () => { calls++; await gate }
  const f = fakeAmp(); await plugin(f.amp); const c = await connect(f, 'T-a', append)
  const p1 = post(c.descriptor, { id: 'same', content: 'note' })
  const p2 = post(c.descriptor, { id: 'same', content: 'note' })
  await new Promise((resolve) => setTimeout(resolve, 20)); assert.equal(calls, 1); release()
  assert.deepEqual([(await p1).status, (await p2).status], [204, 204])
  assert.equal((await post(c.descriptor, { id: 'same', content: 'other' })).status, 409)
  let failures = 0
  const bad = fakeAmp(); await plugin(bad.amp); const d = await connect(bad, 'T-b', async () => { failures++; throw new Error('unknown outcome') })
  assert.equal((await post(d.descriptor, { id: 'failed', content: 'note' })).status, 500)
  assert.equal((await post(d.descriptor, { id: 'failed', content: 'note' })).status, 500)
  assert.equal(failures, 1)
  await Promise.all([...f.disposers, ...bad.disposers].map((dispose) => dispose()))
})
