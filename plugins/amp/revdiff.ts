import type { PluginAPI, PluginCommandContext, PluginThread } from '@ampcode/plugin'

import { randomBytes } from 'node:crypto'
import { mkdtemp, realpath, rm, writeFile } from 'node:fs/promises'
import { createServer, type Server } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

export const description = 'Connect revdiff in a sibling terminal to the current Amp thread.'

const MAX_BODY = 1024 * 1024
const MAX_ID = 256
const GUIDANCE =
  'Revdiff review feedback follows. The user owns staging. Do not stage, unstage, reset, or commit changes without asking first.\n\n'

type Connection = { server: Server; directory: string; descriptor: string }
type Outcome = { content: string; result: Promise<void> }

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`
}

async function showConnection(thread: PluginThread, descriptor: string): Promise<void> {
  await thread.appendUserMessage({
    type: 'user-message',
    content: [
      'Revdiff is connected. Setup information for the human user, not a request for the agent to run commands or edit files.',
      'Run this command in the sibling terminal, in the same Git checkout:',
      '',
      '```sh',
      `${shellQuote('revdiff')} --amp ${shellQuote(descriptor)} --untracked`,
      '```',
      '',
      'If using this fork directly, replace the initial revdiff executable with ./.bin/revdiff.',
      'This command is valid until the plugin disconnects or reloads. Run revdiff: connect again to show the current command.',
    ].join('\n'),
  })
}

async function closeServer(server: Server): Promise<void> {
  if (!server.listening) return
  server.closeAllConnections()
  await new Promise<void>((resolve) => server.close(() => resolve()))
}

export default async function revdiffPlugin(amp: PluginAPI): Promise<void> {
  const connections = new Map<string, Connection>()
  const connecting = new Map<string, Promise<void>>()

  async function disconnect(threadID: string): Promise<boolean> {
    const connection = connections.get(threadID)
    if (!connection) return false
    connections.delete(threadID)
    await rm(connection.directory, { recursive: true, force: true })
    await closeServer(connection.server)
    return true
  }

  async function connect(ctx: PluginCommandContext): Promise<void> {
    if (!ctx.thread) {
      await ctx.ui.notify('Start a thread before connecting revdiff.')
      return
    }
    const existing = connections.get(ctx.thread.id)
    if (existing) {
      await showConnection(ctx.thread, existing.descriptor)
      return
    }
    const workspaceURI = ctx.system.workspaceRoot
    if (!workspaceURI) {
      await ctx.ui.notify('Open a workspace before connecting revdiff.')
      return
    }

    const root = await realpath(amp.helpers.filePathFromURI(workspaceURI))
    const token = randomBytes(32).toString('hex')
    const outcomes = new Map<string, Outcome>()
    const thread: PluginThread = ctx.thread

    const server = createServer((request, response) => {
      void (async () => {
        if (request.method !== 'POST' || request.url !== '/feedback') {
          response.writeHead(404).end('not found')
          return
        }
        if (request.headers.origin !== undefined) {
          response.writeHead(403).end('browser origins are not accepted')
          return
        }
        if (request.headers.authorization !== `Bearer ${token}`) {
          response.writeHead(401).end('unauthorized')
          return
        }

        const chunks: Buffer[] = []
        let size = 0
        for await (const chunk of request) {
          size += chunk.length
          if (size > MAX_BODY) {
            response.writeHead(413).end('body too large')
            return
          }
          chunks.push(chunk)
        }
        let body: unknown
        try {
          body = JSON.parse(Buffer.concat(chunks).toString('utf8'))
        } catch {
          response.writeHead(400).end('invalid JSON')
          return
        }
        if (
          typeof body !== 'object' || body === null || Array.isArray(body) ||
          typeof (body as { id?: unknown }).id !== 'string' ||
          (body as { id: string }).id.length === 0 || (body as { id: string }).id.length > MAX_ID ||
          typeof (body as { content?: unknown }).content !== 'string' ||
          (body as { content: string }).content.trim().length === 0
        ) {
          response.writeHead(400).end('expected a bounded id and nonempty content')
          return
        }
        const { id, content } = body as { id: string; content: string }
        const previous = outcomes.get(id)
        if (previous && previous.content !== content) {
          response.writeHead(409).end('id was already used with different content')
          return
        }
        const outcome = previous ?? {
          content,
          result: thread.appendUserMessage(
            { type: 'user-message', content: GUIDANCE + content },
            { steer: true },
          ),
        }
        if (!previous) outcomes.set(id, outcome)
        try {
          await outcome.result
          response.writeHead(204).end()
        } catch (error) {
          amp.logger.log('revdiff feedback append failed; id remains cached', id, error)
          response.writeHead(500).end('feedback outcome is uncertain; check the thread before reconnecting and resending')
        }
      })().catch((error) => {
        amp.logger.log('revdiff request failed', error)
        if (!response.headersSent) response.writeHead(500)
        response.end('internal error')
      })
    })

    await new Promise<void>((resolve, reject) => {
      server.once('error', reject)
      server.listen(0, '127.0.0.1', () => {
        server.off('error', reject)
        resolve()
      })
    })
    const address = server.address()
    if (!address || typeof address === 'string') throw new Error('revdiff server has no TCP address')
    const directory = await mkdtemp(join(tmpdir(), 'revdiff-amp-')).catch(async (error) => {
      await closeServer(server)
      throw error
    })
    const descriptor = join(directory, 'connection.json')
    try {
      await writeFile(descriptor, JSON.stringify({
        version: 1,
        url: `http://127.0.0.1:${address.port}/feedback`,
        token,
        root,
        thread: thread.id,
      }), { mode: 0o600 })
      connections.set(thread.id, { server, directory, descriptor })
    } catch (error) {
      await closeServer(server)
      await rm(directory, { recursive: true, force: true })
      throw error
    }
    await showConnection(thread, descriptor)
  }

  amp.registerCommand('revdiff-connect', {
    category: 'revdiff', title: 'connect', description: 'Connect sibling-terminal revdiff to this thread',
  }, async (ctx) => {
    if (!ctx.thread) return connect(ctx)
    const id = ctx.thread.id
    const pending = connecting.get(id)
    if (pending) return pending
    const task = connect(ctx)
    connecting.set(id, task)
    try { await task } finally { connecting.delete(id) }
  })
  amp.registerCommand('revdiff-disconnect', {
    category: 'revdiff', title: 'disconnect', description: 'Disconnect revdiff from this thread',
  }, async (ctx) => {
    if (!ctx.thread) return void await ctx.ui.notify('No current thread to disconnect.')
    await connecting.get(ctx.thread.id)
    const removed = await disconnect(ctx.thread.id)
    await ctx.ui.notify(removed ? 'Revdiff disconnected.' : 'Revdiff is not connected to this thread.')
  })
  amp.onDispose(async () => {
    await Promise.allSettled(connecting.values())
    await Promise.all([...connections.keys()].map(disconnect))
  })
}
