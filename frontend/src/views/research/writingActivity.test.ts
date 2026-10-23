import assert from 'node:assert/strict'
import test from 'node:test'
import { canExtractWritingEvent, mergeWritingEventPage, writingEventContentChange, writingEventName, writingLearningStatus } from './writingActivity'
import type { WritingEvent } from '@/api/research'

const event = (kind: string, before = '原稿', after = '改稿'): WritingEvent => ({ id: 'event', page_id: 'draft', kind, base_version: 1, base_content: before, content: after })

test('保存、接受和恢复的真实正文差异都能提炼，不要求先接受AI提案', () => {
  for (const kind of ['save', 'accept', 'revert']) assert.equal(canExtractWritingEvent(event(kind)), true)
  assert.equal(canExtractWritingEvent(event('save', '', '我直接写的新稿')), true)
})

test('提示词、未接受提案和拒绝记录不能当作已采用的写作经验', () => {
  for (const kind of ['create', 'prompt', 'proposal', 'reject', 'generation_failed']) assert.equal(canExtractWritingEvent(event(kind)), false)
  assert.equal(canExtractWritingEvent(event('save', '正文未变', '正文未变')), false)
  assert.equal(canExtractWritingEvent(event('accept', '', '')), false)
})

test('记录和后台学习状态有明确中文名称', () => {
  assert.equal(writingEventName('prompt'), '提交写作要求')
  assert.equal(writingEventName('generation_failed'), '生成失败')
  assert.equal(writingLearningStatus('queued'), '等待提炼')
  assert.equal(writingLearningStatus('failed'), '提炼失败')
})

test('提交提示词和生成失败没有新正文，不将空输出误显示为删除整份旧稿', () => {
  for (const kind of ['prompt', 'generation_failed']) assert.equal(writingEventContentChange(event(kind, '保留的完整旧稿', '')), null)
  assert.equal(writingEventContentChange(event('save', '正文未变', '正文未变')), null)
  assert.deepEqual(writingEventContentChange(event('save', '主动清空的旧稿', '')), { before: '主动清空的旧稿', after: '' })
  assert.deepEqual(writingEventContentChange(event('proposal')), { before: '原稿', after: '改稿' })
})

test('加载更早记录按服务端顺序累积去重；最新页刷新保留已加载历史并更新学习状态', () => {
  const one = { ...event('save'), id: 'one', learning_status: 'queued' }
  const two = { ...event('save'), id: 'two' }
  const three = { ...event('save'), id: 'three' }
  const history = mergeWritingEventPage([one, two], [two, three], true)
  assert.deepEqual(history.map(item => item.id), ['one', 'two', 'three'])
  const refreshed = mergeWritingEventPage(history, [{ ...one, learning_status: 'completed' }])
  assert.deepEqual(refreshed.map(item => item.id), ['one', 'two', 'three'])
  assert.equal(refreshed[0].learning_status, 'completed')
  assert.deepEqual(mergeWritingEventPage(refreshed, [], true), refreshed)
})

test('超过一页新记录且和已加载历史无交集时重建分页窗口，避免跳过中间提示词', () => {
  const previous = [{ ...event('save'), id: 'old' }]
  const newest = Array.from({ length: 100 }, (_, index) => ({ ...event('prompt'), id: `new-${index}` }))
  assert.deepEqual(mergeWritingEventPage(previous, newest), newest)
})
