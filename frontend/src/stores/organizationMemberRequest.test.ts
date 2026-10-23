import assert from 'node:assert/strict'
import test from 'node:test'
import { createOrganizationMemberRequest } from './organizationMemberRequest.ts'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

type Member = { id: string }
type Response = { success: boolean; data?: { members: Member[] }; message?: string }
function setup() {
  const requests: ReturnType<typeof deferred<Response>>[] = []
  const state = { currentMembers: [] as Member[], membersLoading: false, error: null as string | null }
  const loader = createOrganizationMemberRequest<Member>({
    request: () => { const result = deferred<Response>(); requests.push(result); return result.promise },
    setMembers: members => { state.currentMembers = members },
    setError: error => { state.error = error },
    setLoading: loading => { state.membersLoading = loading },
  })
  return { requests, state, loader }
}
const response = (id: string): Response => ({ success: true, data: { members: [{ id }] } })

test('switching organizations cannot let the old response replace currentMembers', async () => {
  const { loader, requests, state } = setup()
  const first = loader.fetch('organization-a')
  const second = loader.fetch('organization-b')
  requests[1].resolve(response('member-b'))
  await second
  requests[0].resolve(response('member-a'))
  await first
  assert.deepEqual(state.currentMembers, [{ id: 'member-b' }])
  assert.equal(state.membersLoading, false)
})

test('an old response or failure cannot hide the active member loading state or set its error', async () => {
  const { loader, requests, state } = setup()
  const first = loader.fetch('organization-a')
  const second = loader.fetch('organization-b')
  requests[0].reject(new Error('organization a unavailable'))
  await first
  assert.equal(state.membersLoading, true)
  assert.equal(state.error, null)
  assert.deepEqual(state.currentMembers, [])
  requests[1].resolve(response('member-b'))
  await second
  assert.equal(state.membersLoading, false)
})

test('refreshing the same organization keeps only the most recent response', async () => {
  const { loader, requests, state } = setup()
  const first = loader.fetch('organization-a')
  const second = loader.fetch('organization-a')
  requests[1].resolve(response('updated-member'))
  await second
  requests[0].resolve(response('outdated-member'))
  await first
  assert.deepEqual(state.currentMembers, [{ id: 'updated-member' }])
})

test('clearing organization context invalidates an in-flight member response', async () => {
  const { loader, requests, state } = setup()
  const pending = loader.fetch('organization-a')
  loader.invalidate()
  requests[0].resolve(response('member-a'))
  await pending
  assert.deepEqual(state.currentMembers, [])
  assert.equal(state.membersLoading, false)
  assert.equal(state.error, null)
})

test('setting a different detail context clears stale members but preserves a matching in-flight request', async () => {
  const { loader, requests, state } = setup()
  const first = loader.fetch('organization-a')
  requests[0].resolve(response('member-a'))
  await first
  loader.setContext('organization-b')
  assert.deepEqual(state.currentMembers, [])
  const second = loader.fetch('organization-b')
  loader.setContext('organization-b')
  requests[1].resolve(response('member-b'))
  await second
  assert.deepEqual(state.currentMembers, [{ id: 'member-b' }])
})

test('a current failed response remains actionable and always releases member loading', async () => {
  const { loader, requests, state } = setup()
  const pending = loader.fetch('organization-a')
  requests[0].resolve({ success: false, message: 'Access has changed' })
  await pending
  assert.equal(state.error, 'Access has changed')
  assert.equal(state.membersLoading, false)
})

test('mutation context checks follow the active organization and expire when it is cleared', async () => {
  const { loader, requests } = setup()
  const first = loader.fetch('organization-a')
  requests[0].resolve(response('shared-tenant'))
  await first
  assert.equal(loader.matchesContext('organization-a'), true)
  const second = loader.fetch('organization-b')
  assert.equal(loader.matchesContext('organization-a'), false)
  assert.equal(loader.matchesContext('organization-b'), true)
  requests[1].resolve(response('shared-tenant'))
  await second
  loader.invalidate()
  assert.equal(loader.matchesContext('organization-b'), false)
})
