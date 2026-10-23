import assert from 'node:assert/strict'
import test from 'node:test'
import { collectionDisplayName, resolveWorkspaceLocation as resolve, sectionSourceBases as sections, type WorkspaceBase } from './workspaceNavigation'

const resolveWorkspaceLocation: typeof resolve = (q, own, shared, inboxId = 'inbox') => resolve(q, own, shared, inboxId)
const sectionSourceBases: typeof sections = (section, own, shared, project = '', inboxId = 'inbox') => sections(section, own, shared, project, inboxId)

const own: WorkspaceBase[] = [
  { id: 'inbox', name: '我的收件箱' },
  { id: 'robotics', name: '具身智能' },
  { id: 'other', name: '行业研究' },
  { id: 'drafts', wiki_config: { research_only: true } },
  { id: 'faq', type: 'faq' },
]
const shared = [
  { organization_id: 'alpha', knowledge_base: { id: 'team-a' } },
  { organization_id: 'beta', knowledge_base: { id: 'team-b' } },
  { organization_id: 'beta', knowledge_base: { id: 'team-a' } },
  { organization_id: 'alpha', knowledge_base: { id: 'team-drafts', wiki_config: { research_only: true } } },
]

test('the personal library includes the bound inbox exactly once and excludes dedicated drafts', () => {
  assert.deepEqual(sectionSourceBases('library', own, shared).map(base => base.id), ['inbox', 'robotics', 'other'])
  assert.deepEqual(resolveWorkspaceLocation({ section: 'library', kb: 'inbox' }, own, shared), {
    section: 'library', kb: 'inbox', project: '',
  })
})

test('an inbox-only account can open its collection through the single personal entry', () => {
  assert.equal(resolveWorkspaceLocation({ section: 'library', kb: 'inbox' }, own.slice(0, 1), []).kb, 'inbox')
})

test('legacy inbox links normalize to the personal library and preserve valid collection IDs', () => {
  assert.deepEqual(resolveWorkspaceLocation({ section: 'inbox', kb: 'inbox' }, own, shared), {
    section: 'library', kb: 'inbox', project: '',
  })
  assert.equal(resolveWorkspaceLocation({ section: 'inbox', kb: 'robotics' }, own, shared).kb, 'robotics')
})

test('legacy collection links retain their correct section without requiring a new query parameter', () => {
  assert.equal(resolveWorkspaceLocation({ kb: 'inbox' }, own, shared).section, 'library')
  assert.equal(resolveWorkspaceLocation({ kb: 'robotics' }, own, shared).section, 'library')
  assert.equal(resolveWorkspaceLocation({ kb: 'team-a' }, own, shared).section, 'team')
})

test('project filters restrict the selected collection and collapse duplicate shares', () => {
  assert.deepEqual(sectionSourceBases('team', own, shared).map(base => base.id), ['team-a', 'team-b'])
  assert.deepEqual(resolveWorkspaceLocation({ section: 'team', kb: 'team-b', project: 'alpha' }, own, shared), {
    section: 'team', kb: 'team-a', project: 'alpha',
  })
})

test('unavailable collections and malformed query values never become active data sources', () => {
  assert.equal(resolveWorkspaceLocation({ section: 'library', kb: 'deleted' }, own, shared).kb, 'inbox')
  assert.equal(resolveWorkspaceLocation({ section: ['team'], kb: ['team-a'] }, own, shared).section, 'library')
  assert.equal(resolveWorkspaceLocation({ section: 'team', project: 'missing' }, own, shared).kb, '')
  assert.equal(resolveWorkspaceLocation({}, [], []).kb, '')
})

test('switching back to library keeps the last selected personal collection', () => {
  assert.equal(resolveWorkspaceLocation({ section: 'library', kb: 'other' }, own, shared).kb, 'other')
})


test('renaming the bound inbox or creating a namesake cannot redirect personal intake', () => {
  const renamed = [{ id: 'decoy', name: '我的收件箱' }, { id: 'inbox', name: '我的素材' }]
  assert.equal(resolveWorkspaceLocation({}, renamed, []).kb, 'inbox')
  assert.deepEqual(sectionSourceBases('library', renamed, []).map(k => k.id), ['decoy', 'inbox'])
  assert.equal(collectionDisplayName(renamed[0]!, 'inbox'), '我的收件箱')
  assert.equal(collectionDisplayName(renamed[1]!, 'inbox'), '我的素材')
  assert.equal(resolveWorkspaceLocation({}, renamed, [], '').kb, 'decoy')
})
