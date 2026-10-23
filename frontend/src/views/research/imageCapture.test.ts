import assert from 'node:assert/strict'
import test from 'node:test'
import { imageCaptureOverrides, imageRecognitionConfigured, isCaptureImage } from './imageCapture'
import { sourceState } from './workspaceState'

test('images without a visual model require explicit archive consent, never silent downgrade', () => {
  const image = { name: 'robot.png', type: 'image/png' }
  assert.throws(() => imageCaptureOverrides(image, 'recognize', false), /主动选择/)
  assert.deepEqual(imageCaptureOverrides(image, 'archive', false), { image_archive_only: true })
})

test('mixed batches apply archive intent only to images, preserving PDF and document parsing', () => {
  const files = [{ name: 'figure.PNG' }, { name: 'paper.pdf' }, { name: 'note.md' }]
  assert.deepEqual(files.map(file => imageCaptureOverrides(file, 'archive', false)), [{ image_archive_only: true }, undefined, undefined])
  assert.equal(isCaptureImage({ name: 'clipboard', type: 'image/png' }), true)
})

test('recognition requires an enabled visual model and explicitly disables archive-only processing', () => {
  assert.equal(imageRecognitionConfigured(undefined), false)
  assert.equal(imageRecognitionConfigured({ vlm_config: { enabled: true } }), false)
  assert.equal(imageRecognitionConfigured({ vlm_config: { enabled: false, model_id: 'vision' } }), false)
  assert.equal(imageRecognitionConfigured({ vlm_config: { enabled: true, model_id: 'vision' } }), true)
  assert.deepEqual(imageCaptureOverrides({ name: 'robot.webp' }, 'recognize', true), { image_archive_only: false })
})

test('archived originals never claim recognized searchable text or satisfy writing selection readiness', () => {
  const archived = sourceState({ id: 'image', parse_status: 'archived' })
  assert.equal(archived.label, '原图已归档 · 未识别')
  assert.notEqual(archived.tone, 'success')
})
