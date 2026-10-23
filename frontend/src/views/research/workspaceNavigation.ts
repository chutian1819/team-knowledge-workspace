export type WorkspaceSection = 'library' | 'team'

export interface WorkspaceBase {
  id: string
  name?: string
  type?: string
  wiki_config?: { research_only?: boolean }
}

export interface SharedWorkspaceBase {
  organization_id?: string
  knowledge_base: WorkspaceBase
}

export interface WorkspaceLocation {
  section: WorkspaceSection
  kb: string
  project: string
}

export function personalSourceBases<T extends WorkspaceBase>(bases: T[]): T[] {
  return bases.filter(base => base.type !== 'faq' && !base.wiki_config?.research_only)
}

export function sectionSourceBases<T extends WorkspaceBase>(
  section: WorkspaceSection,
  ownBases: T[],
  sharedBases: Array<{ organization_id?: string; knowledge_base: T }>,
  project = '',
  inboxId = '',
): T[] {
  const personal = personalSourceBases(ownBases)
  if (section === 'library') return personal
  return [...new Map(sharedBases
    .filter(item => (!project || item.organization_id === project)
      && item.knowledge_base.type !== 'faq' && !item.knowledge_base.wiki_config?.research_only)
    .map(item => [item.knowledge_base.id, item.knowledge_base])).values()]
}

/** Both direct links and UI switches resolve within the chosen section. */
export function resolveWorkspaceLocation(
  query: Record<string, unknown>,
  ownBases: WorkspaceBase[],
  sharedBases: SharedWorkspaceBase[],
  inboxId = '',
): WorkspaceLocation {
  const requestedKb = typeof query.kb === 'string' ? query.kb : ''
  const project = typeof query.project === 'string' ? query.project : ''
  const personal = personalSourceBases(ownBases)
  const inbox = personal.find(base => base.id === inboxId)
  let section: WorkspaceSection
  // Old inbox URLs keep their bound collection while using the single personal entry.
  if (query.section === 'inbox' || query.section === 'library') section = 'library'
  else if (query.section === 'team') section = 'team'
  else if (!requestedKb || requestedKb === inbox?.id) section = 'library'
  else section = personal.some(base => base.id === requestedKb) ? 'library'
    : sharedBases.some(item => item.knowledge_base.id === requestedKb) ? 'team' : 'library'
  const candidates = sectionSourceBases(section, ownBases, sharedBases, project, inboxId)
  return { section, project: section === 'team' ? project : '', kb: candidates.some(base => base.id === requestedKb) ? requestedKb : (section === 'library' ? inbox?.id : '') || candidates[0]?.id || '' }
}

/** The intake binding stays private and keeps its ID, even after a rename. */
export function collectionDisplayName(base: WorkspaceBase, inboxId: string): string {
  return base.id === inboxId && ['我的收件箱', '个人收件箱', '未分类'].includes(base.name || '')
    ? '未分类' : base.name || '未命名资料集'
}
