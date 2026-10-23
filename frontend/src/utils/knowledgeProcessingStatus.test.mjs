import test from 'node:test'
import assert from 'node:assert/strict'

import {
  parseStatusToTimelineStatus,
  resolveTimelineHeaderStatus,
  shouldShowProcessingError,
  isHardTerminalKnowledgeStatus,
} from './knowledgeProcessingStatus.ts'

test('failed attempts expose their cause while awaiting an automatic retry', () => {
  assert.equal(shouldShowProcessingError(true, 'processing', 'failed'), true)
  assert.equal(shouldShowProcessingError(true, 'failed', 'done'), true)
  assert.equal(shouldShowProcessingError(true, 'processing', 'running'), false)
  assert.equal(shouldShowProcessingError(false, 'failed', 'failed'), false)
})

test('archive-only completion stops timeline polling and retains its distinct status', () => {
  assert.equal(isHardTerminalKnowledgeStatus('archived'), true)
  assert.equal(isHardTerminalKnowledgeStatus('finalizing'), false)
  assert.equal(shouldShowProcessingError(true, 'archived', 'failed'), false)
  assert.equal(resolveTimelineHeaderStatus({ parseStatus: 'archived', traceStatus: 'done', isLatestAttempt: true }), 'archived')
})

test('latest attempt uses failed knowledge status even when root span is done', () => {
  assert.equal(resolveTimelineHeaderStatus({
    parseStatus: 'failed',
    traceStatus: 'done',
    isLatestAttempt: true,
  }), 'failed')
})

test('latest attempt keeps finalizing distinct from a completed root span', () => {
  assert.equal(resolveTimelineHeaderStatus({
    parseStatus: 'finalizing',
    traceStatus: 'done',
    isLatestAttempt: true,
  }), 'finalizing')
})

test('historical attempt keeps its own root status', () => {
  assert.equal(resolveTimelineHeaderStatus({
    parseStatus: 'failed',
    traceStatus: 'done',
    isLatestAttempt: false,
  }), 'done')
})

test('parse status maps completed and processing into timeline vocabulary', () => {
  assert.equal(parseStatusToTimelineStatus('completed'), 'done')
  assert.equal(parseStatusToTimelineStatus('processing'), 'running')
})
