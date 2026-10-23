import assert from 'node:assert/strict'
import test from 'node:test'
import { collectSources, existingSourceFromConflict } from './captureFlow'

const source = { id: 'original', knowledge_base_id: 'shared-project', title: '原资料', custom_metadata: { research_source_person: '最初来源' } }

test('native flattened duplicate URL points to its existing project and settles the URL', async () => {
  let pendingUrl = 'https://example.com/paper'
  let existing: unknown
  const counts = await collectSources([pendingUrl], async () => {
    throw { status: 409, success: false, code: 'duplicate_url', data: source }
  }, {
    settled: () => { pendingUrl = '' },
    created: async () => { assert.fail('duplicate must never update provenance') },
    existing: value => { existing = value },
  })
  assert.equal(pendingUrl, '')
  assert.equal(existing, source)
  assert.deepEqual(counts, { created: 0, existing: 1 })
  assert.equal(source.custom_metadata.research_source_person, '最初来源')
})

test('a duplicate file does not stop later uploads or overwrite existing source metadata', async () => {
  const pending = ['duplicate.pdf', 'new.pdf']
  const metadataWrites: string[] = []
  const uploaded: string[] = []
  const counts = await collectSources([...pending], async file => {
    uploaded.push(file)
    if (file === 'duplicate.pdf') throw { status: 409, code: 'duplicate_file', data: source }
    return { success: true, data: { id: 'new', knowledge_base_id: 'inbox' } }
  }, {
    settled: () => { pending.shift() },
    created: async response => { metadataWrites.push(response.data.id) },
    existing: () => {},
  })
  assert.deepEqual(uploaded, ['duplicate.pdf', 'new.pdf'])
  assert.deepEqual(metadataWrites, ['new'])
  assert.deepEqual(pending, [])
  assert.deepEqual(counts, { created: 1, existing: 1 })
})

test('permission failures and unrelated conflicts remain errors with their pending inputs intact', async () => {
  for (const error of [
    { status: 403, code: 'duplicate_file', data: source },
    { status: 409, code: 'version_conflict', data: source },
    { status: 409, code: 'duplicate_file', data: { id: 'unlocated' } },
  ]) {
    assert.equal(existingSourceFromConflict(error), null)
    const pending = ['failed.pdf', 'later.pdf']
    await assert.rejects(collectSources([...pending], async () => { throw error }, {
      settled: () => { pending.shift() },
      created: async () => { assert.fail('must not update metadata') },
      existing: () => { assert.fail('must not expose an existing source') },
    }), value => value === error)
    assert.deepEqual(pending, ['failed.pdf', 'later.pdf'])
  }
})

test('a later network failure retains only the unprocessed files after a duplicate', async () => {
  const pending = ['duplicate.pdf', 'failed.pdf', 'later.pdf']
  const networkError = { message: 'network unavailable' }
  await assert.rejects(collectSources([...pending], async file => {
    if (file === 'duplicate.pdf') throw { status: 409, code: 'duplicate_file', data: source }
    throw networkError
  }, {
    settled: () => { pending.shift() },
    created: async () => { assert.fail('no new metadata should be written') },
    existing: () => {},
  }), value => value === networkError)
  assert.deepEqual(pending, ['failed.pdf', 'later.pdf'])
})
