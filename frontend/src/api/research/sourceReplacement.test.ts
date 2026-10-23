import assert from 'node:assert/strict'
import test from 'node:test'
import { sourceReplacementRequest } from './sourceReplacement'
import { uploadTimeoutMs, UPLOAD_TIMEOUT_FLOOR_MS } from '../../utils/requestTimeouts'

test('a replacement targets the existing source identity and uses the native multipart field', async () => {
  const file = new File(['new evidence'], '项目更新.pdf', { type: 'application/pdf' })
  const request = sourceReplacementRequest('kb-1', 'existing-knowledge', file)
  assert.equal(request.url, '/api/v1/research/capture/kb-1/sources/existing-knowledge/file')
  assert.deepEqual([...request.data.keys()], ['file'])
  const original = request.data.get('file') as File
  assert.equal(original.name, '项目更新.pdf')
  assert.equal(await original.text(), 'new evidence')
  assert.ok(uploadTimeoutMs(request.data) >= UPLOAD_TIMEOUT_FLOOR_MS)
})
test('replacement request identities cannot change route boundaries', () => {
  const request = sourceReplacementRequest('kb/1', 'source?2', new File(['x'], 'x.txt'))
  assert.equal(request.url, '/api/v1/research/capture/kb%2F1/sources/source%3F2/file')
})
