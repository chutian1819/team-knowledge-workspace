import assert from 'node:assert/strict'
import test from 'node:test'
import { createRouteChunkRecovery, isRouteChunkLoadError } from './routeChunkRecovery.ts'

const staleChunk = () => new TypeError('Failed to fetch dynamically imported module: https://example.test/assets/Login-old.js')

function fixture() {
  const values = new Map<string, string>()
  const calls: string[] = []
  let now = 1_000_000
  let online = true
  const environment = {
    isOnline: () => online,
    getStorage: () => ({
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => { values.set(key, value) },
    }),
    now: () => now,
    resolveHref: (fullPath: string) => `/workspace${fullPath}`,
    replace: (href: string) => { calls.push(href) },
  }
  return {
    values,
    calls,
    environment,
    recover: createRouteChunkRecovery(environment),
    advance: (milliseconds: number) => { now += milliseconds },
    setOnline: (value: boolean) => { online = value },
  }
}

test('recognizes native import and Vite stylesheet resource failures only', () => {
  for (const message of [
    staleChunk().message,
    'error loading dynamically imported module: https://example.test/assets/Settings.js',
    'Importing a module script failed.',
    'Unable to preload CSS for /assets/Login.css',
  ]) {
    assert.equal(isRouteChunkLoadError(new Error(message)), true, message)
  }
  for (const error of [
    new Error('Network Error'),
    new TypeError("Cannot read properties of null (reading 'id')"),
    new SyntaxError("Unexpected token '<'"),
    new Error('Guard failed: Failed to fetch dynamically imported module'),
    null,
    'Failed to fetch dynamically imported module',
    { message: 42 },
  ]) {
    assert.equal(isRouteChunkLoadError(error), false)
  }
})

test('recovers to the pending route with base path, query and hash intact', () => {
  const f = fixture()
  const target = '/login?redirect=%2Fplatform%2Fknowledge-bases#sign-in'
  assert.equal(f.recover(staleChunk(), target), true)
  assert.deepEqual(f.calls, [`/workspace${target}`])
  assert.equal(f.values.size, 1)
})

test('persists the cooldown across a fresh document and permits a later retry', () => {
  const f = fixture()
  assert.equal(f.recover(staleChunk(), '/login'), true)

  const afterReload = createRouteChunkRecovery(f.environment)
  assert.equal(afterReload(staleChunk(), '/login'), false)
  f.advance(29_999)
  assert.equal(afterReload(staleChunk(), '/login'), false)
  f.advance(1)
  assert.equal(afterReload(staleChunk(), '/login'), true)
  assert.deepEqual(f.calls, ['/workspace/login', '/workspace/login'])
})

test('cooldown is scoped to the complete route target', () => {
  const f = fixture()
  assert.equal(f.recover(staleChunk(), '/login?redirect=one#form'), true)
  assert.equal(f.recover(staleChunk(), '/login?redirect=two#form'), true)
  assert.equal(f.recover(staleChunk(), '/login?redirect=one#form'), false)
  assert.equal(f.calls.length, 2)
})

test('offline failures neither navigate nor consume the retry', () => {
  const f = fixture()
  f.setOnline(false)
  assert.equal(f.recover(staleChunk(), '/login'), false)
  assert.equal(f.values.size, 0)
  assert.deepEqual(f.calls, [])
  f.setOnline(true)
  assert.equal(f.recover(staleChunk(), '/login'), true)
})

test('runtime errors do not navigate or write a retry marker', () => {
  const f = fixture()
  assert.equal(f.recover(new TypeError('Unexpected application state'), '/login'), false)
  assert.equal(f.values.size, 0)
  assert.deepEqual(f.calls, [])
})

for (const failure of ['access', 'read', 'write', 'ignored-write'] as const) {
  test(`unavailable session storage (${failure}) never triggers navigation`, () => {
    const f = fixture()
    const recover = createRouteChunkRecovery({
      ...f.environment,
      getStorage: () => {
        if (failure === 'access') throw new Error('Storage is blocked')
        return {
          getItem: () => {
            if (failure === 'read') throw new Error('Storage read failed')
            return null
          },
          setItem: () => {
            if (failure === 'write') throw new Error('Storage write failed')
          },
        }
      },
    })
    assert.equal(recover(staleChunk(), '/login'), false)
    assert.deepEqual(f.calls, [])
  })
}

test('a failed browser navigation is contained and still throttles retries', () => {
  const f = fixture()
  let attempts = 0
  const recover = createRouteChunkRecovery({
    ...f.environment,
    replace: () => {
      attempts += 1
      throw new Error('Navigation unavailable')
    },
  })
  assert.equal(recover(staleChunk(), '/login'), false)
  assert.equal(recover(staleChunk(), '/login'), false)
  assert.equal(attempts, 1)
})
