<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter, onBeforeRouteLeave } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { listModels, type ModelConfig } from '@/api/model'
import { createKnowledgeBase, getKnowledgeDetails } from '@/api/knowledge-base'
import type { WikiPage } from '@/api/wiki'
import ChatReferenceSourceView from '@/components/ChatReferenceSourceView.vue'
import type { ReferenceSourceTarget } from '@/utils/referenceSources'
import { renderDocumentPreviewMarkdown } from '@/utils/documentPreviewMarkdown'
import { localizeResearchCitations, parseResearchCitation, researchSourceRefs } from './researchCitations'
import { diffWikiLines } from '@/utils/wikiLineDiff'
import * as api from '@/api/research'
import { errorMessage, jobLabel, terminalJob } from './workspaceState'
import { defaultDraftSourceScope, canCreateWithScope, draftCreationPayload, type DraftSourceScope } from './draftCreation'
import WritingExperiencePanel from './WritingExperiencePanel.vue'
import { applicableWritingRules, writingRuleSelection } from './writingExperience'
import { researchModelId } from './modelSelection'

const props = defineProps<{ sourceKbId: string; selectedIds: string[]; knowledgeBases: any[] }>()
const emit = defineEmits<{ refresh: [] }>()
const router = useRouter()
const auth = useAuthStore()
const draftKbId = ref('')
const draftKbs = computed(() => props.knowledgeBases.filter(kb => kb.wiki_config?.research_only))
const drafts = ref<WikiPage[]>([])
const templates = ref<api.ResearchTemplate[]>([])
const models = ref<ModelConfig[]>([])
const draft = ref<WikiPage | null>(null)
const title = ref('')
const content = ref('')
const newTitle = ref('')
const templateId = ref('')
const sourceScope = ref<DraftSourceScope>(defaultDraftSourceScope(props.selectedIds))
const createSourceKbId = ref(props.sourceKbId)
const createRetrievalQuery = ref('')
const reviseRetrieval = ref(false)
const reviseRetrievalQuery = ref('')
const reviseRetrievalKbId = ref(props.sourceKbId)
const createRuleIds = ref<string[]>([])
const createRules = ref<api.WritingRule[]>([])
const createRulesLoading = ref(false)
const createInstruction = ref('根据所选知识库资料和文种要求撰写完整初稿，逐项标注证据，区分事实、判断和待核实问题。')
const createAutoLearning = ref(true)
const modelId = ref('')
const instruction = ref('根据所选资料补充这份研究稿，逐项标注证据，区分事实、判断和待核实问题。')
const proposals = ref<api.Proposal[]>([])
const writingRuleIds = ref<string[]>([])
const writingActivityTick = ref(0)
const proposal = ref<api.Proposal | null>(null)
const busy = ref('')
const error = ref('')
const conflict = ref(false)
const revisions = ref<any[]>([])
const showRevisions = ref(false)
const exportJobs = ref<api.ExportJob[]>([])
const createVisible = ref(false)
const savedTitle = ref('')
const preview = ref(false)
const sourceTarget = ref<ReferenceSourceTarget | null>(null)
let generation = 0
let sourceRequest = 0
let createRulesRequest = 0
let exportTimer: ReturnType<typeof setTimeout> | undefined
const canEdit = computed(() => auth.hasRole('contributor') && props.knowledgeBases.find(kb => kb.id === draftKbId.value)?.research_permission !== 'viewer')
const dirty = computed(() => !!draft.value && (title.value !== draft.value.title || content.value !== draft.value.content))
const missingModelKey = computed(() => { const model = models.value.find(item => item.id === modelId.value); return model?.source === 'remote' && model.credentials?.api_key?.configured === false })
const pendingProposals = computed(() => proposals.value.filter(item => item.status === 'pending'))
const diff = computed(() => proposal.value ? diffWikiLines(proposal.value.base_content, proposal.value.content) : [])
const diffCounts = computed(() => ({
  added: diff.value.filter(line => line.type === 'add').length,
  removed: diff.value.filter(line => line.type === 'del').length,
}))
const sourceKbs = computed(() => props.knowledgeBases.filter(kb => !kb.wiki_config?.research_only))
const retrievalEvidence = computed<api.RetrievalEvidence | undefined>(() => draft.value?.page_metadata?.source_versions?._retrieval)
const vectorReady = (id: string) => { const kb = sourceKbs.value.find(item => item.id === id); return !!kb?.indexing_strategy?.vector_enabled && !!kb?.embedding_model_id }
const createRetrievalReady = computed(() => vectorReady(createSourceKbId.value))
const reviseRetrievalReady = computed(() => !reviseRetrieval.value || (vectorReady(reviseRetrievalKbId.value) && !!reviseRetrievalQuery.value.trim()))
const createSourceIds = computed(() => createSourceKbId.value === props.sourceKbId ? props.selectedIds : [])
const createAvailableRules = computed(() => applicableWritingRules(createRules.value, templateId.value))
const canCreate = computed(() => canEdit.value && !!newTitle.value.trim() && !!draftKbId.value && canCreateWithScope(sourceScope.value, createSourceIds.value) && (sourceScope.value === 'template' || !!createSourceKbId.value) && (sourceScope.value !== 'retrieval' || (createRetrievalReady.value && !!createRetrievalQuery.value.trim())))
const canCreateAndGenerate = computed(() => canCreate.value && !!modelId.value && !missingModelKey.value && !!createInstruction.value.trim() && !createRulesLoading.value)
const writingActivityVersion = computed(() => `${writingActivityTick.value}:${draft.value?.version}:${proposals.value.map(item => `${item.id}:${item.status}`).join(',')}`)
const currentSourceIds = computed<string[]>(() => draft.value?.page_metadata?.source_ids || draft.value?.source_refs || [])
const sourceRefs = computed(() => researchSourceRefs(draft.value?.source_refs || []))
const retrievalSourceTitle = (knowledgeId: string) => sourceRefs.value.find(source => source.id === knowledgeId)?.title || '未命名来源'
const renderedDraft = computed(() => renderDocumentPreviewMarkdown(localizeResearchCitations(content.value)))
function templateOutline(template: api.ResearchTemplate) {
  return template.content.split('\n').filter(line => /^#{1,6}\s/.test(line)).slice(0, 3).map(line => line.replace(/^#{1,6}\s+/, '')).join(' · ') || (template.content.trim() ? '按预设结构撰写' : '空白文档')
}
function revisionTime(value?: string) {
  if (!value) return '时间未记录'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', year: 'numeric', hour12: false })
}
async function showSource(knowledgeId: string, chunkId?: string) {
  const request = ++sourceRequest
  try { const response = await getKnowledgeDetails(knowledgeId); if (request !== sourceRequest) return; const item = response.data; sourceTarget.value = { knowledgeId, chunkId, knowledgeBaseId: item.knowledge_base_id, title: item.title || item.file_name, fileName: item.file_name } }
  catch (err) { handleError(err) }
}
function followCitation(event: MouseEvent) {
  const anchor = (event.target as Element)?.closest('a'); if (!anchor) return
  const target = parseResearchCitation(anchor.getAttribute('href') || ''); if (!target) return
  event.preventDefault(); void showSource(target.knowledgeId, target.chunkId)
}
function closeSource() { sourceRequest++; sourceTarget.value = null }
type DraftDialog = 'source' | 'create' | 'proposal' | 'revisions'
function closeDraftDialog(dialog: DraftDialog) {
  if (busy.value) return
  if (dialog === 'source') closeSource()
  else if (dialog === 'create') createVisible.value = false
  else if (dialog === 'proposal') proposal.value = null
  else showRevisions.value = false
}
// TDesign checks KeyboardEvent.code only. Some browser/assistive inputs supply
// key without code. Run after document handlers so nested popups get first use.
function closeDraftDialogOnEscape(event: KeyboardEvent) {
  if (event.defaultPrevented || busy.value || (event.key !== 'Escape' && event.code !== 'Escape')) return
  const dialog: DraftDialog | undefined = sourceTarget.value ? 'source' : proposal.value ? 'proposal'
    : createVisible.value ? 'create' : showRevisions.value ? 'revisions' : undefined
  if (!dialog) return
  event.preventDefault(); event.stopPropagation(); closeDraftDialog(dialog)
}
onBeforeRouteLeave(() => !dirty.value || window.confirm('当前有未保存的编辑，离开后将丢弃。是否继续？'))
defineExpose({ dirty, busy })
function handleError(err: unknown) { error.value = errorMessage(err); conflict.value = (err as any)?.status === 409 }
function applyDraft(page: WikiPage) { draft.value = page; reviseRetrieval.value = false; reviseRetrievalQuery.value = retrievalEvidence.value?.query || ''; reviseRetrievalKbId.value = retrievalEvidence.value?.knowledge_base_id || props.sourceKbId; title.value = page.title; content.value = page.content; drafts.value = drafts.value.map(item => item.id === page.id ? page : item); conflict.value = false; error.value = ''; if (!canEdit.value) preview.value = true }
async function initialize() {
  const token = ++generation
  draft.value = null; drafts.value = []; proposal.value = null; exportJobs.value = []; proposals.value = []; writingRuleIds.value = []; error.value = ''; conflict.value = false
  if (!draftKbId.value) return
  busy.value = 'load'
  try {
    const results = await Promise.all([api.listDrafts(draftKbId.value), api.listTemplates(draftKbId.value), listModels('KnowledgeQA')])
    if (token !== generation) return
    drafts.value = results[0].data || []
    templates.value = results[1].data || []
    templateId.value = templates.value[0]?.id || ''
    models.value = results[2] || []
    modelId.value = researchModelId(models.value, draftKbs.value.find(kb => kb.id === draftKbId.value))
  } catch (err) { if (token === generation) handleError(err) }
  finally { if (token === generation) busy.value = '' }
}
async function openDraft(id: string) {
  if (dirty.value && !window.confirm('当前有未保存的编辑。离开后会丢弃这些编辑，是否继续？')) return
  const token = ++generation; busy.value = 'load'; error.value = ''; proposal.value = null
  try {
    const [page, changes, exports] = await Promise.all([api.getDraft(draftKbId.value, id), api.listProposals(draftKbId.value, id), api.listExports(draftKbId.value, id)])
    if (token !== generation) return
    applyDraft(page.data); proposals.value = changes.data || []; writingRuleIds.value = []; exportJobs.value = exports.data || []; showRevisions.value = false; void pollExports()
  } catch (err) { if (token === generation) handleError(err) }
  finally { if (token === generation) busy.value = '' }
}
async function createDraftKb() {
  busy.value = 'setup'; error.value = ''
  try {
    const source = props.knowledgeBases.find(item => item.id === props.sourceKbId)
    const availableModels = await listModels('KnowledgeQA')
    const synthesisModel = availableModels.find(item => item.id === source?.summary_model_id) || availableModels.find(item => item.is_default) || availableModels[0]
    const result = await createKnowledgeBase({
      name: `${source?.name || '项目'} · 研究稿`, description: '研究稿、修改提案与版本记录。', type: 'document',
      summary_model_id: synthesisModel?.id,
      wiki_config: { research_only: true, synthesis_model_id: synthesisModel?.id },
      indexing_strategy: { vector_enabled: false, keyword_enabled: false, wiki_enabled: true, graph_enabled: false },
    })
    emit('refresh'); draftKbId.value = result.data.id
    MessagePlugin.success('研究稿资料集已建立，可在共享设置中添加项目成员。')
  } catch (err) { handleError(err) }
  finally { busy.value = '' }
}
async function openCreate() {
  createSourceKbId.value = props.sourceKbId
  createRetrievalQuery.value = ''
  sourceScope.value = props.selectedIds.length ? 'selected' : (props.sourceKbId ? 'all' : 'template')
  createRuleIds.value = []
  templateId.value = templates.value[0]?.id || ''
  createAutoLearning.value = true
  createVisible.value = true
  const request = ++createRulesRequest
  const kbId = draftKbId.value
  createRules.value = []; createRulesLoading.value = true
  try {
    const response = await api.listWritingRules(kbId)
    if (request === createRulesRequest && kbId === draftKbId.value) createRules.value = response.data || []
  } catch (err) { if (request === createRulesRequest) handleError(err) }
  finally { if (request === createRulesRequest) createRulesLoading.value = false }
}
function toggleCreateRule(id: string, checked: boolean) {
  createRuleIds.value = writingRuleSelection(checked ? [...createRuleIds.value, id] : createRuleIds.value.filter(item => item !== id), createRules.value, templateId.value)
}
async function create(generateInitial = false) {
  if (!canCreate.value || (generateInitial && !canCreateAndGenerate.value)) return
  busy.value = 'create'; error.value = ''
  try {
    const response = await api.createDraft(draftKbId.value, draftCreationPayload(
      newTitle.value, templateId.value, sourceScope.value, createSourceIds.value, createSourceKbId.value, createRetrievalQuery.value,
    ))
    applyDraft(response.data); drafts.value.unshift(response.data); proposals.value = []; writingRuleIds.value = writingRuleSelection(createRuleIds.value, createRules.value, templateId.value); proposal.value = null; exportJobs.value = []; showRevisions.value = false; createVisible.value = false; newTitle.value = ''
    instruction.value = createInstruction.value.trim() || instruction.value
    if (createAutoLearning.value && modelId.value && !missingModelKey.value) {
      try { await api.saveWritingLearning(draftKbId.value, response.data.id, { enabled: true, model_id: modelId.value }) }
      catch (err) { MessagePlugin.warning(`研究稿已创建，自动学习暂未开启：${errorMessage(err)}`) }
    }
    if (generateInitial) {
      busy.value = 'generate'
      const result = await api.createProposal(draftKbId.value, response.data.id, { base_version: response.data.version, instruction: instruction.value, model_id: modelId.value, writing_rule_ids: [...writingRuleIds.value] })
      proposal.value = result.data; proposals.value.unshift(result.data)
    } else MessagePlugin.success('研究稿已创建，可继续撰写')
  } catch (err) { handleError(err) }
  finally { busy.value = ''; writingActivityTick.value++ }
}
async function save() {
  if (!draft.value || !canEdit.value) return
  busy.value = 'save'; error.value = ''
  try {
    const response = await api.saveDraft(draftKbId.value, draft.value.id, { base_version: draft.value.version, title: title.value, content: content.value })
    applyDraft(response.data); drafts.value = drafts.value.map(item => item.id === response.data.id ? response.data : item)
    MessagePlugin.success('已保存新版本')
  } catch (err) { handleError(err) }
  finally { busy.value = '' }
}
async function generate() {
  if (!draft.value || !canEdit.value || dirty.value || !modelId.value || missingModelKey.value || !instruction.value.trim() || !reviseRetrievalReady.value) return
  busy.value = 'generate'; error.value = ''
  try {
    const response = await api.createProposal(draftKbId.value, draft.value.id, { base_version: draft.value.version, instruction: instruction.value, model_id: modelId.value, writing_rule_ids: [...writingRuleIds.value], ...(reviseRetrieval.value ? { retrieval_query: reviseRetrievalQuery.value.trim(), retrieval_kb_id: reviseRetrievalKbId.value } : {}) })
    proposal.value = response.data; proposals.value.unshift(response.data)
  } catch (err) { handleError(err) }
  finally { busy.value = ''; writingActivityTick.value++ }
}
async function decide(accept: boolean) {
  if (!draft.value || !proposal.value || !canEdit.value || dirty.value) return
  busy.value = 'decide'; error.value = ''
  try {
    const id = proposal.value.id
    if (accept) applyDraft((await api.acceptProposal(draftKbId.value, draft.value.id, id)).data)
    else await api.rejectProposal(draftKbId.value, draft.value.id, id)
    proposals.value = proposals.value.map(item => item.id === id ? { ...item, status: accept ? 'accepted' : 'rejected' } : item); proposal.value = null
    MessagePlugin.success(accept ? '修改已保存为新版本' : '提案已拒绝')
  } catch (err) { handleError(err) }
  finally { busy.value = '' }
}
async function history() {
  if (!draft.value) return
  busy.value = 'history'
  try { const result = await api.listDraftRevisions(draftKbId.value, draft.value.id); revisions.value = Array.isArray(result.data) ? result.data : result.data?.revisions || result.revisions || []; showRevisions.value = true }
  catch (err) { handleError(err) } finally { busy.value = '' }
}
async function revert(version: number) {
  if (!draft.value || !canEdit.value || dirty.value) return
  busy.value = 'revert'
  try { applyDraft((await api.revertDraft(draftKbId.value, draft.value.id, version, draft.value.version)).data); showRevisions.value = false }
  catch (err) { handleError(err) } finally { busy.value = '' }
}
async function pollExports() {
  if (exportTimer) clearTimeout(exportTimer)
  const kb = draftKbId.value; const pageId = draft.value?.id
  const jobs = exportJobs.value.filter(item => !terminalJob(item.status))
  if (!jobs.length) return
  try {
    const updates = await Promise.all(jobs.map(item => api.getDraftExport(kb, item.id)))
    if (kb !== draftKbId.value || pageId !== draft.value?.id) return
    for (const update of updates) exportJobs.value = exportJobs.value.map(item => item.id === update.data.id ? update.data : item)
  } catch (err) { handleError(err); return }
  if (exportJobs.value.some(item => !terminalJob(item.status))) exportTimer = setTimeout(pollExports, 2500)
}
async function exportFile(format: string) {
  if (!draft.value || dirty.value) return
  busy.value = 'export'; savedTitle.value = draft.value.title
  try { exportJobs.value.unshift((await api.exportDraft(draftKbId.value, draft.value.id, format)).data); void pollExports() }
  catch (err) { handleError(err) } finally { busy.value = '' }
}
async function download(job: api.ExportJob) {
  try { api.downloadBlob(await api.downloadDraftExport(draftKbId.value, job.id), job.filename || job.file_name || `${savedTitle.value || draft.value?.title || '研究稿'}.${job.format}`) }
  catch (err) { handleError(err) }
}
function preserveLocal() { api.downloadBlob(new Blob([content.value], { type: 'text/markdown;charset=utf-8' }), `${title.value || '未保存稿'}.md`) }
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
watch(draftKbs, value => { if (!draftKbId.value && value.length) draftKbId.value = value[0].id }, { immediate: true })
watch(draftKbId, initialize, { immediate: true })
watch(templateId, () => { createRuleIds.value = writingRuleSelection(createRuleIds.value, createRules.value, templateId.value) })
watch(createSourceKbId, value => { if (value !== props.sourceKbId && sourceScope.value === 'selected') sourceScope.value = value ? 'all' : 'template' })
window.addEventListener('beforeunload', beforeUnload)
window.addEventListener('keydown', closeDraftDialogOnEscape)
onBeforeUnmount(() => { generation++; sourceRequest++; createRulesRequest++; if (exportTimer) clearTimeout(exportTimer); window.removeEventListener('beforeunload', beforeUnload); window.removeEventListener('keydown', closeDraftDialogOnEscape) })
</script>

<template>
  <section class="draft-workspace" aria-label="研究稿工作区">
    <div class="draft-toolbar">
      <div class="workspace-heading"><h2>研究稿</h2><p>撰写文稿、审阅修改和管理版本。</p></div>
      <div class="toolbar-controls">
        <t-select v-if="draftKbs.length" v-model="draftKbId" class="draft-kb-select" aria-label="研究稿资料集" placeholder="选择研究稿资料集" :disabled="!!busy || dirty">
          <t-option v-for="kb in draftKbs" :key="kb.id" :value="kb.id" :label="kb.name" />
        </t-select>
        <t-button v-if="draftKbId && canEdit" :disabled="!!busy || dirty" @click="openCreate"><template #icon><t-icon name="add" /></template>新建研究稿</t-button>
      </div>
    </div>
    <t-alert v-if="error" theme="error" :message="error" class="alert" />
    <div v-if="conflict" class="conflict-box" role="alert">
      <strong>版本或访问权限已变化，当前编辑仍保留。</strong>
      <div class="row"><t-button variant="outline" @click="preserveLocal">下载当前编辑</t-button><t-button variant="outline" @click="draft && openDraft(draft.id)">载入最新版本</t-button></div>
    </div>
    <div v-if="!draftKbId" class="draft-empty setup-empty">
      <div class="empty-icon"><t-icon name="file-copy" size="28px" /></div>
      <div class="empty-copy"><h3>建立研究稿资料集</h3><p>集中保存研究文稿、引用来源与修改记录。</p></div>
      <t-button :disabled="!auth.hasRole('contributor') || !!busy" :loading="busy === 'setup'" @click="createDraftKb">建立资料集</t-button>
      <p v-if="!auth.hasRole('contributor')" class="muted">请联系项目管理员开通编辑权限。</p>
    </div>
    <div v-else class="draft-layout" :class="{ 'has-draft': draft && busy !== 'load' }" :aria-busy="busy === 'load'">
      <aside class="draft-list" aria-label="研究稿列表">
        <div class="list-label"><span>全部文稿</span><span class="count-badge">{{ drafts.length }}</span></div>
        <div class="draft-list-items">
          <button v-for="page in drafts" :key="page.id" type="button" class="draft-item" :class="{ active: draft?.id === page.id }" :aria-current="draft?.id === page.id ? 'page' : undefined" :disabled="!!busy" @click="openDraft(page.id)">
            <t-icon name="file" size="17px" /><span class="draft-item-copy"><span :title="page.title">{{ page.title }}</span><small>版本 {{ page.version }}</small></span>
          </button>
          <p v-if="!drafts.length && !busy" class="muted empty-list">暂无研究稿</p>
        </div>
        <t-button class="list-settings" variant="text" size="small" @click="router.push(`/platform/knowledge-bases/${draftKbId}`)"><template #icon><t-icon name="setting" /></template>资料集与共享</t-button>
      </aside>
      <div v-if="busy === 'load'" class="draft-loading" role="status" aria-live="polite"><t-loading size="small" /><span>正在加载研究稿</span></div>
      <div v-else-if="draft" class="editor-area">
        <div class="document-meta"><span class="document-type"><t-icon name="file" />研究稿</span><span v-if="!canEdit" class="readonly-badge">只读</span><span class="version" :class="{ unsaved: dirty }" role="status">v{{ draft.version }} · {{ dirty ? '未保存' : '已保存' }}</span></div>
        <div class="editor-head"><t-input v-model="title" class="title-input" aria-label="研究稿标题" :disabled="!!busy || !canEdit" /></div>
        <div class="editor-actions">
          <div class="view-switch" aria-label="文稿显示方式"><button type="button" :class="{ active: !preview }" :aria-pressed="!preview" @click="preview = false">{{ canEdit ? '编辑' : '源文' }}</button><button type="button" :class="{ active: preview }" :aria-pressed="preview" @click="preview = true">预览</button></div>
          <div class="row"><t-button size="small" variant="text" :disabled="!!busy || dirty" @click="history"><template #icon><t-icon name="history" /></template>版本记录</t-button><t-button size="small" :disabled="!dirty || !!busy || !canEdit" :loading="busy === 'save'" @click="save">保存修改</t-button></div>
        </div>
        <div v-if="preview" class="rendered-draft" @click="followCitation" v-html="renderedDraft" />
        <textarea v-else v-model="content" class="markdown-editor" aria-label="研究稿正文，支持 Markdown" placeholder="开始撰写正文…" spellcheck="false" :disabled="!!busy" :readonly="!canEdit" />
        <div class="document-footer"><span><t-icon name="link" />{{ currentSourceIds.length }} 份来源</span><span v-if="!preview">Markdown</span></div>
        <div class="export-row"><span>{{ dirty ? '保存后可导出' : '导出文稿' }}</span><t-button v-for="format in ['md', 'docx', 'pdf']" :key="format" size="small" variant="outline" :disabled="!!busy || dirty" @click="exportFile(format)">{{ format.toUpperCase() }}</t-button></div>
        <div v-if="exportJobs.length" class="export-jobs" aria-live="polite"><div v-for="job in exportJobs" :key="job.id" class="export-job"><span>{{ job.format?.toUpperCase() }} · {{ jobLabel(job.status) }}</span><span v-if="job.error" class="danger">{{ job.error }}</span><t-button v-if="job.status === 'ready'" size="small" variant="text" @click="download(job)">下载</t-button><t-button v-else-if="job.status === 'failed'" size="small" variant="text" :disabled="!!busy || dirty" @click="exportFile(job.format || 'md')">重试</t-button><t-button v-else size="small" variant="text" @click="pollExports">刷新状态</t-button></div></div>
      </div>
      <div v-else class="draft-empty">
        <div class="empty-icon"><t-icon name="edit" size="28px" /></div>
        <div class="empty-copy"><h3>{{ drafts.length ? '选择一份研究稿' : '新建第一份研究稿' }}</h3><p>{{ drafts.length ? '从左侧列表打开文稿，继续编辑或查看历史版本。' : '选择模板开始撰写，并关联本项目的资料。' }}</p></div>
        <t-button v-if="!drafts.length && canEdit" variant="outline" :disabled="!!busy" @click="openCreate">新建研究稿</t-button>
      </div>
      <aside v-if="draft && busy !== 'load'" class="assistant-panel" aria-label="AI 修订与引用来源">
        <div class="assistant-heading"><span class="assistant-icon"><t-icon name="edit" size="18px" /></span><div><h3>AI 修订</h3><p>修改经审阅后保存</p></div></div>
        <t-alert v-if="!models.length" theme="warning" message="暂无可用模型，可继续手动编辑。" />
        <t-button v-if="!models.length || missingModelKey" variant="outline" @click="router.push('/platform/settings?section=models')">前往模型设置</t-button>
        <label v-if="models.length" class="field-label">生成模型<t-select v-model="modelId" aria-label="生成模型" :disabled="!!busy || !canEdit"><t-option v-for="model in models" :key="model.id" :value="model.id" :label="model.display_name || model.name" /></t-select></label>
        <t-alert v-if="missingModelKey" theme="warning" message="请在模型设置中配置 API 密钥。" />
        <label class="field-label">修改要求<t-textarea v-model="instruction" placeholder="例如：补充研究方法，并核对引用来源" aria-label="AI 修改要求" :autosize="{ minRows: 5, maxRows: 10 }" :disabled="!!busy || !canEdit" /></label>
        <div v-if="currentSourceIds.length" class="retrieval-controls">
          <label class="retrieval-toggle"><input v-model="reviseRetrieval" type="checkbox" :disabled="!!busy || !canEdit" /><span>本轮重新检索已关联资料</span></label>
          <template v-if="reviseRetrieval"><label class="field-label">检索资料集<t-select v-model="reviseRetrievalKbId" aria-label="本轮检索资料集" :disabled="!!busy"><t-option v-for="kb in sourceKbs" :key="kb.id" :value="kb.id" :label="kb.name" :disabled="!vectorReady(kb.id)" /></t-select></label><label class="field-label">检索问题<t-textarea v-model="reviseRetrievalQuery" aria-label="本轮语义检索问题" :maxlength="1000" :autosize="{ minRows: 2, maxRows: 5 }" :disabled="!!busy" placeholder="需要从已关联来源核实什么？" /></label><p class="muted">只检索本稿已关联资料，请选择这些资料所在资料集。</p></template>
          <p v-else-if="retrievalEvidence" class="muted">沿用已保存的检索证据；需要其他片段时可重新检索。</p>
        </div>
        <WritingExperiencePanel :key="`${draftKbId}:${draft.id}`" v-model="writingRuleIds" :kb-id="draftKbId" :draft-id="draft.id" :template-id="draft.page_metadata?.template_id || ''" :model-id="modelId" :can-generate="!!modelId && !missingModelKey" :can-edit="canEdit" :disabled="!!busy" :templates="templates" :proposals="proposals" :activity-version="writingActivityVersion" />
        <p v-if="dirty" class="save-hint"><t-icon name="info-circle" />请先保存当前编辑。</p>
        <t-button block :loading="busy === 'generate'" :disabled="!!busy || dirty || !canEdit || !modelId || missingModelKey || !instruction.trim() || !reviseRetrievalReady" @click="generate">{{ busy === 'generate' ? '正在生成修改…' : '生成修改提案' }}</t-button>
        <div v-if="pendingProposals.length" class="pending-list"><h4>待审提案<span class="count-badge">{{ pendingProposals.length }}</span></h4><button v-for="item in pendingProposals" :key="item.id" type="button" :disabled="!!busy || dirty" @click="proposal = item"><span class="proposal-label">基于 v{{ item.base_version }} 的修改<t-icon name="chevron-right" /></span><small>查看并审阅差异</small></button></div>
        <details v-if="retrievalEvidence" class="retrieval-evidence"><summary>检索证据 · {{ retrievalEvidence.results.length }} 个片段</summary><p>{{ retrievalEvidence.query }}</p><small>向量检索 · {{ revisionTime(retrievalEvidence.captured_at) }}</small><button v-for="hit in retrievalEvidence.results" :key="hit.chunk_id" type="button" :aria-label="`查看检索片段 ${hit.rank} · ${retrievalSourceTitle(hit.knowledge_id)}`" :title="retrievalSourceTitle(hit.knowledge_id)" @click="showSource(hit.knowledge_id, hit.chunk_id)"><span class="retrieval-hit-copy"><strong>{{ retrievalSourceTitle(hit.knowledge_id) }}</strong><small>片段 {{ hit.rank }} · {{ hit.chunk_type === 'image_ocr' ? '图中文字' : hit.chunk_type === 'image_caption' ? '图像描述' : '正文' }}</small></span><t-icon name="chevron-right" /></button></details>
        <div v-if="sourceRefs.length" class="source-references"><h4>引用来源<span class="count-badge">{{ sourceRefs.length }}</span></h4><button v-for="source in sourceRefs" :key="source.id" type="button" @click="showSource(source.id)"><t-icon name="file" /><span>{{ source.title }}</span><t-icon name="chevron-right" /></button></div>
        <div class="evidence-note"><t-icon name="link" /><span>引用来源需保持可访问；资料不足时，请先补充来源。</span></div>
      </aside>
    </div>
    <t-dialog :visible="!!sourceTarget" header="引用来源" width="min(1100px, 94vw)" :footer="false" :close-on-esc-keydown="!busy" :close-on-overlay-click="!busy" @close="closeDraftDialog('source')"><template #closeBtn><t-button size="small" variant="text" shape="square" aria-label="关闭引用来源" :disabled="!!busy" @click.stop="closeDraftDialog('source')"><t-icon name="close" aria-hidden="true" /></t-button></template><div class="source-preview"><ChatReferenceSourceView v-if="sourceTarget" :target="sourceTarget" :active="!!sourceTarget" @unavailable="error = '此来源无法预览，请在原文资料集中查看。'" /></div></t-dialog>
    <t-dialog :visible="createVisible" header="新建研究稿" width="min(720px, 94vw)" :footer="false" :close-on-overlay-click="!busy" :close-on-esc-keydown="!busy" @close="closeDraftDialog('create')">
      <template #closeBtn><t-button size="small" variant="text" shape="square" aria-label="关闭新建研究稿" :disabled="!!busy" @click.stop="closeDraftDialog('create')"><t-icon name="close" aria-hidden="true" /></t-button></template>
      <div class="modal-form">
        <t-alert v-if="error" theme="error" :message="error" />
        <label class="field-label">文稿标题<t-input :disabled="!!busy" v-model="newTitle" aria-label="新研究稿标题" placeholder="填写研究稿标题" /></label>
        <fieldset v-if="templates.length" class="template-field"><legend>选择模板</legend><div class="template-options"><button v-for="item in templates" :key="item.id" type="button" :class="{ selected: templateId === item.id }" :aria-pressed="templateId === item.id" :disabled="!!busy" @click="templateId = item.id"><span class="template-name"><t-icon name="file" />{{ item.name }}<t-icon v-if="templateId === item.id" name="check-circle-filled" class="template-check" /></span><span class="template-outline">{{ templateOutline(item) }}</span></button></div></fieldset>
        <label class="field-label">来源知识库<t-select v-model="createSourceKbId" aria-label="写作来源知识库" placeholder="选择写作依据的知识库" :disabled="!!busy"><t-option v-for="kb in sourceKbs" :key="kb.id" :value="kb.id" :label="kb.name" /></t-select></label>
        <fieldset class="scope-field"><legend>关联资料</legend><t-radio-group class="evidence-scope-options" aria-label="关联资料范围" :disabled="!!busy" v-model="sourceScope"><t-radio value="all" :disabled="!createSourceKbId">全部已解析资料（完整阅读）</t-radio><t-radio value="retrieval" :disabled="!createRetrievalReady">整库语义检索</t-radio><t-radio value="selected" :disabled="!createSourceIds.length">当前勾选的 {{ createSourceIds.length }} 份资料</t-radio><t-radio value="template">暂不关联</t-radio></t-radio-group><p class="muted">{{ sourceScope === 'template' ? '可先编辑文稿，再补充来源。' : sourceScope === 'retrieval' ? '根据问题从资料集中选取最多 8 个相关片段；检索问题和证据随稿保存。' : '每份研究稿最多关联 100 份资料；完整阅读超过上下文预算时会提示缩小范围。' }}</p></fieldset>
        <p v-if="createSourceKbId && !createRetrievalReady" class="muted">语义检索需先在来源资料集启用向量索引、配置 Embedding 模型并完成索引。</p>
        <label v-if="sourceScope === 'retrieval'" class="field-label">检索问题<t-textarea v-model="createRetrievalQuery" aria-label="整库语义检索问题" :maxlength="1000" :autosize="{ minRows: 3, maxRows: 5 }" :disabled="!!busy" placeholder="例如：具身智能的通用策略如何跨机器人迁移？现有证据与局限是什么？" /></label>
        <fieldset class="scope-field"><legend>搭配写作经验 <span class="muted">选填 · {{ createRuleIds.length }}/5</span></legend><p v-if="createRulesLoading" class="muted" role="status">正在加载可用经验…</p><p v-else-if="!createAvailableRules.length" class="muted">当前文种暂无已确认经验，可直接开始撰写。</p><label v-for="rule in createAvailableRules" :key="rule.id" class="create-rule-option"><input type="checkbox" :checked="createRuleIds.includes(rule.id)" :disabled="!!busy || (!createRuleIds.includes(rule.id) && createRuleIds.length >= 5)" @change="toggleCreateRule(rule.id, ($event.target as HTMLInputElement).checked)" /><span>{{ rule.name }}<small>{{ rule.instruction }}</small></span></label></fieldset>
        <label class="field-label">写作要求<t-textarea v-model="createInstruction" aria-label="首次写作要求" placeholder="描述目标读者、核心内容和表达要求" :autosize="{ minRows: 3, maxRows: 6 }" :disabled="!!busy" /></label>
        <label v-if="models.length" class="field-label">写作模型<t-select v-model="modelId" aria-label="首次写作模型" :disabled="!!busy"><t-option v-for="model in models" :key="model.id" :value="model.id" :label="model.display_name || model.name" /></t-select></label>
        <div class="create-learning"><label><input v-model="createAutoLearning" type="checkbox" :disabled="!!busy" /><span>自动学习我的写作习惯</span></label><p class="muted">提交的提示词和保存的版本差异会自动记录。开启后，正文保存或接受修改时在后台提炼候选，确认后才会用于后续写作。</p><p v-if="!modelId || missingModelKey" class="muted">当前模型不可用：可以先建稿并记录修改，配置模型后开启自动提炼。</p></div>
        <div class="create-footer"><t-button variant="outline" :disabled="!!busy || !canCreate" :loading="busy === 'create'" @click="create(false)">仅创建文稿</t-button><t-button :disabled="!!busy || !canCreateAndGenerate" :loading="busy === 'generate' || busy === 'create'" @click="create(true)">创建并撰写初稿</t-button></div>
      </div>
    </t-dialog>
    <t-dialog :visible="!!proposal" header="审阅修改提案" width="min(1050px, 94vw)" :footer="false" :close-on-esc-keydown="!busy" :close-on-overlay-click="!busy" @close="closeDraftDialog('proposal')">
      <template #closeBtn><t-button size="small" variant="text" shape="square" aria-label="关闭修改提案" :disabled="!!busy" @click.stop="closeDraftDialog('proposal')"><t-icon name="close" aria-hidden="true" /></t-button></template>
      <template v-if="proposal"><t-alert v-if="error" theme="error" :message="error" /><div class="diff-summary"><span>基于版本 {{ proposal.base_version }}</span><span class="diff-count added">+ {{ diffCounts.added }} 行新增</span><span class="diff-count removed">− {{ diffCounts.removed }} 行删除</span></div><p v-if="proposal.writing_rules?.length" class="proposal-rules">本次使用：{{ proposal.writing_rules.map(rule => `${rule.name} v${rule.version}`).join('、') }}</p><p v-if="proposal.source_versions?._retrieval" class="proposal-rules">检索问题：{{ proposal.source_versions._retrieval.query }} · {{ proposal.source_versions._retrieval.results.length }} 个证据片段</p><pre class="proposal-diff" aria-label="修改差异"><span v-for="(line, index) in diff" :key="index" :class="line.type">{{ line.type === 'add' ? '+' : line.type === 'del' ? '−' : ' ' }} {{ line.text }}
</span></pre><div class="diff-footer"><t-button theme="default" variant="outline" :disabled="!!busy || dirty || !canEdit" @click="decide(false)">拒绝提案</t-button><t-button :loading="busy === 'decide'" :disabled="!!busy || dirty || !canEdit" @click="decide(true)">接受并保存</t-button></div></template>
    </t-dialog>
    <t-dialog :visible="showRevisions" header="版本记录" width="min(560px, 94vw)" :footer="false" :close-on-esc-keydown="!busy" :close-on-overlay-click="!busy" @close="closeDraftDialog('revisions')"><template #closeBtn><t-button size="small" variant="text" shape="square" aria-label="关闭版本记录" :disabled="!!busy" @click.stop="closeDraftDialog('revisions')"><t-icon name="close" aria-hidden="true" /></t-button></template><p class="history-note muted">恢复历史版本将另存为新版本，保留全部修改记录。</p><div v-if="!revisions.length" class="history-empty"><t-icon name="history" size="24px" /><p>暂无历史版本</p></div><div v-for="revision in revisions" :key="revision.id || revision.version" class="revision-row"><div class="revision-marker"><t-icon name="history" /></div><div class="revision-copy"><strong>版本 {{ revision.version }}<span v-if="revision.version === draft?.version" class="current-revision">当前版本</span></strong><time>{{ revisionTime(revision.edited_at || revision.created_at) }}</time></div><t-popconfirm :content="`将当前稿件恢复为 v${revision.version}？`" @confirm="revert(revision.version)"><t-button size="small" variant="text" :disabled="!!busy || dirty || !canEdit || revision.version === draft?.version">恢复</t-button></t-popconfirm></div></t-dialog>
  </section>
</template>

<style scoped lang="less">
.draft-workspace { padding-top: 8px; }
.retrieval-controls { display: grid; gap: 12px; .muted { font-size: var(--app-text-xs); } }
.retrieval-toggle { display: flex; gap: 8px; align-items: flex-start; font-size: var(--app-text-sm); line-height: 1.7; cursor: pointer; input { accent-color: var(--td-brand-color); margin-top: 4px; } }
.retrieval-evidence { border-top: 1px solid var(--app-border-subtle); padding-top: 16px; font-size: var(--app-text-sm); line-height: 1.8; min-width: 0; summary { cursor: pointer; font-weight: 500; } p { margin-top: 8px; overflow-wrap: anywhere; } small { color: var(--td-text-color-secondary); } button { display: flex; justify-content: space-between; align-items: center; gap: 8px; width: 100%; padding: 8px 0; border: 0; background: transparent; text-align: left; color: var(--td-text-color-link); cursor: pointer; } }

.retrieval-hit-copy { display: grid; gap: 2px; min-width: 0; flex: 1; strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; } small { font-size: var(--app-text-xs); } }
.retrieval-evidence button > .t-icon { flex-shrink: 0; }
.proposal-rules { margin: 12px 0; font-size: var(--app-text-sm); line-height: 1.7; color: var(--td-text-color-secondary); }
h2, h3, h4, p { margin: 0; }
button { font: inherit; }
button:disabled { cursor: not-allowed; }
button:focus-visible { outline: 2px solid var(--td-text-color-link); outline-offset: 3px; }
.draft-toolbar { display: flex; justify-content: space-between; gap: 20px; align-items: center; margin-bottom: 24px; }
.workspace-heading { h2 { font-size: var(--app-text-3xl); font-weight: 600; letter-spacing: -.4px; } p { color: var(--td-text-color-secondary); font-size: var(--app-text-md); margin-top: 7px; line-height: 1.6; } }
.toolbar-controls, .row { display: flex; gap: 10px; align-items: center; }
.draft-kb-select { width: 220px; }
.muted { color: var(--td-text-color-secondary); font-size: var(--app-text-md); line-height: 1.7; }
.alert { margin-bottom: 16px; }
.draft-layout { display: grid; grid-template-columns: 204px minmax(0, 1fr); border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-panel); overflow: hidden; min-height: 680px; background: var(--app-surface-paper); box-shadow: var(--app-shadow-md); &.has-draft { grid-template-columns: 204px minmax(0, 1fr) 284px; } }
.draft-list { display: flex; flex-direction: column; min-width: 0; padding: 26px 12px 14px; border-right: 1px solid var(--app-border-subtle); background: var(--app-surface-muted); }
.list-label { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: var(--app-text-sm); font-weight: 500; color: var(--td-text-color-secondary); padding: 0 9px 18px; }
.count-badge { display: inline-flex; align-items: center; justify-content: center; min-width: 22px; height: 20px; padding: 0 5px; box-sizing: border-box; border-radius: var(--app-radius-xs); background: var(--td-bg-color-secondarycontainer); color: var(--td-text-color-secondary); font-size: var(--app-text-xs); font-weight: 500; font-variant-numeric: tabular-nums; }
.draft-list-items { flex: 1; }
.draft-item { border: 1px solid transparent; background: transparent; color: var(--td-text-color-secondary); display: flex; align-items: flex-start; width: 100%; padding: 12px 10px; gap: 9px; border-radius: var(--app-radius-md); text-align: left; cursor: pointer; margin-bottom: 5px; transition: background-color var(--app-motion-fast), border-color var(--app-motion-fast); > .t-icon { margin-top: 2px; flex-shrink: 0; } &:hover:not(:disabled) { background: var(--td-bg-color-container-hover); } &.active { background: var(--app-surface-raised); color: var(--td-text-color-link); border-color: var(--app-border-subtle); box-shadow: var(--app-shadow-sm), inset 2px 0 0 var(--td-brand-color); } &:disabled { opacity: .65; } }
.draft-item-copy { display: flex; flex-direction: column; gap: 5px; min-width: 0; > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: var(--app-text-md); font-weight: 500; } small { color: var(--td-text-color-secondary); font-size: var(--app-text-xs); font-variant-numeric: tabular-nums; } }
.empty-list { padding: 12px 10px 24px; }
.list-settings { margin-top: 24px; align-self: flex-start; }
.editor-area { padding: 32px 36px 24px; display: flex; flex-direction: column; min-width: 0; background: var(--app-surface-paper); }
.document-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; color: var(--td-text-color-secondary); font-size: var(--app-text-xs); margin-bottom: 14px; }
.document-type { display: inline-flex; align-items: center; gap: 5px; font-weight: 500; }
.version { margin-left: auto; font-variant-numeric: tabular-nums; &.unsaved { color: var(--td-warning-color-7); } }
.readonly-badge { padding: 2px 6px; border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-xs); }
.editor-head { min-width: 0; }
.title-input { :deep(.t-input) { height: 44px; padding: 0 8px; margin-left: -8px; width: calc(100% + 16px); border-color: transparent; background: transparent; } :deep(.t-input:hover) { border-color: var(--td-component-border); } :deep(.t-input--focused) { border-color: var(--td-text-color-link); } :deep(.t-input__inner) { font-size: var(--app-text-4xl); font-weight: 600; letter-spacing: -.025em; color: var(--td-text-color-primary); } }
.editor-actions { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; justify-content: space-between; padding: 16px 0; border-bottom: 1px solid var(--app-border-subtle); }
.view-switch { display: flex; padding: 3px; gap: 2px; border-radius: var(--app-radius-md); background: var(--app-surface-muted); button { border: 0; border-radius: var(--app-radius-sm); background: transparent; color: var(--td-text-color-secondary); padding: 6px 12px; min-height: 30px; font-size: var(--app-text-sm); cursor: pointer; transition: color var(--app-motion-fast), background-color var(--app-motion-fast), box-shadow var(--app-motion-fast); &:hover { color: var(--td-text-color-primary); background: var(--td-bg-color-container-hover); } &.active { color: var(--td-text-color-primary); background: var(--app-surface-raised); box-shadow: var(--app-shadow-sm); } } }
.markdown-editor { min-height: 380px; flex: 1; width: 100%; box-sizing: border-box; background: transparent; color: var(--td-text-color-primary); border: 0; border-radius: var(--app-radius-sm); resize: vertical; padding: 28px 12px; outline: none; font: var(--app-text-base)/var(--app-leading-reading) var(--app-font-family-mono); overflow-wrap: anywhere; &:focus-visible { box-shadow: inset 2px 0 0 var(--td-text-color-link); } &::placeholder { color: var(--td-text-color-placeholder); } &:disabled { opacity: .8; } }
.rendered-draft { min-height: 380px; padding: 28px 0; font-family: var(--app-font-family-reading); font-size: var(--app-text-xl); line-height: var(--app-leading-reading); overflow: auto; overflow-wrap: anywhere; flex: 1; :deep(a) { color: var(--td-text-color-link); text-underline-offset: 3px; } :deep(h1), :deep(h2), :deep(h3) { font-family: var(--app-font-family); font-weight: 600; letter-spacing: -.025em; line-height: 1.5; margin: 1.5em 0 .7em; } :deep(h1) { font-size: var(--app-text-4xl); } :deep(h2) { font-size: var(--app-text-3xl); } :deep(h3) { font-size: var(--app-text-2xl); } :deep(:first-child) { margin-top: 0; } :deep(p) { margin: 0 0 1em; } :deep(table) { border-collapse: collapse; width: 100%; } :deep(td), :deep(th) { border: 1px solid var(--app-border-subtle); padding: 9px 12px; } :deep(th) { background: var(--app-surface-muted); font-family: var(--app-font-family); font-weight: 500; } :deep(code) { font-family: var(--app-font-family-mono); font-size: .875em; } :deep(img) { max-width: 100%; height: auto; border-radius: var(--app-radius-md); } :deep(blockquote) { margin: 18px 0; padding: 8px 16px; border-left: 3px solid var(--td-brand-color); color: var(--td-text-color-secondary); background: var(--app-surface-muted); } :deep(pre) { padding: 14px; overflow: auto; border-radius: var(--app-radius-md); background: var(--app-surface-muted); } }
.document-footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; color: var(--td-text-color-secondary); font-size: var(--app-text-xs); padding: 6px 0 18px; > span { display: inline-flex; align-items: center; gap: 5px; } }
.export-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding-top: 18px; border-top: 1px solid var(--app-border-subtle); > span { flex: 1; font-size: var(--app-text-sm); color: var(--td-text-color-secondary); } }
.export-jobs { margin-top: 12px; padding: 4px 10px; background: var(--app-surface-muted); border-radius: var(--app-radius-md); }
.export-job { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; min-height: 36px; font-size: var(--app-text-sm); > .t-button { margin-left: auto; } .danger { flex-basis: 100%; overflow-wrap: anywhere; } }
.assistant-panel { background: var(--app-surface-muted); border-left: 1px solid var(--app-border-subtle); padding: 30px 22px 22px; display: flex; flex-direction: column; gap: 18px; min-width: 0; }
.assistant-heading { display: flex; gap: 10px; align-items: center; padding-bottom: 2px; h3 { font-size: var(--app-text-base); font-weight: 600; } p { color: var(--td-text-color-secondary); font-size: var(--app-text-xs); margin-top: 5px; } }
.assistant-icon { display: flex; align-items: center; justify-content: center; width: 36px; height: 36px; border-radius: var(--app-radius-lg); color: var(--td-text-color-link); border: 1px solid var(--app-border-subtle); background: var(--app-surface-raised); }
.field-label { display: grid; gap: 9px; font-size: var(--app-text-md); font-weight: 500; color: var(--td-text-color-primary); :deep(.t-textarea__inner) { font-size: var(--app-text-md); font-weight: 400; line-height: 1.8; } }
.save-hint { display: flex; align-items: center; gap: 6px; font-size: var(--app-text-sm); color: var(--td-warning-color-7); }
.pending-list, .source-references { padding-top: 19px; border-top: 1px solid var(--app-border-subtle); h4 { display: flex; align-items: center; justify-content: space-between; font-size: var(--app-text-md); font-weight: 500; margin-bottom: 12px; } }
.pending-list button { width: 100%; border: 1px solid var(--app-border-subtle); background: var(--app-surface-raised); padding: 12px; border-radius: var(--app-radius-md); text-align: left; color: var(--td-text-color-primary); cursor: pointer; margin-bottom: 7px; transition: border-color var(--app-motion-fast); &:hover:not(:disabled) { border-color: var(--td-text-color-link); } &:disabled { opacity: .6; } small { display: block; font-size: var(--app-text-xs); color: var(--td-text-color-secondary); margin-top: 7px; } }
.proposal-label { display: flex; align-items: center; justify-content: space-between; gap: 6px; font-size: var(--app-text-sm); }
.source-references button { border: 0; background: transparent; color: var(--td-text-color-secondary); text-align: left; padding: 8px 0; display: flex; align-items: center; gap: 7px; width: 100%; font-size: var(--app-text-sm); cursor: pointer; > span { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } > .t-icon { flex-shrink: 0; } &:hover { color: var(--td-text-color-link); } }
.evidence-note { display: flex; align-items: flex-start; gap: 7px; font-size: var(--app-text-xs); line-height: 1.8; color: var(--td-text-color-secondary); margin-top: auto; padding-top: 16px; > .t-icon { flex-shrink: 0; margin-top: 3px; } }
.draft-empty { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 22px; text-align: center; min-height: 380px; padding: 48px 30px; color: var(--td-text-color-secondary); box-sizing: border-box; }
.setup-empty { border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-panel); background: var(--app-surface-paper); box-shadow: var(--app-shadow-md); min-height: 420px; }
.empty-icon { display: flex; align-items: center; justify-content: center; width: 64px; height: 64px; border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-xl); color: var(--td-text-color-link); background: var(--app-surface-muted); }
.empty-copy { h3 { color: var(--td-text-color-primary); font-size: var(--app-text-2xl); font-weight: 600; margin-bottom: 10px; } p { max-width: 380px; line-height: 1.8; font-size: var(--app-text-md); } }
.draft-loading { display: flex; justify-content: center; align-items: center; gap: 12px; min-height: 380px; color: var(--td-text-color-secondary); font-size: var(--app-text-md); }
.source-preview { height: 65vh; }
.modal-form { display: grid; gap: 25px; max-height: 72dvh; overflow-y: auto; padding: 4px 6px 8px 0; }
.create-rule-option { display: flex; gap: 9px; align-items: flex-start; margin-top: 11px; font-size: var(--app-text-md); line-height: 1.7; cursor: pointer; input { margin-top: 6px; accent-color: var(--td-brand-color); } span { min-width: 0; overflow-wrap: anywhere; } small { display: block; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); } }
.create-learning { padding: 14px 16px; background: var(--app-surface-muted); border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-md); > label { display: flex; align-items: center; gap: 9px; font-size: var(--app-text-md); cursor: pointer; input { accent-color: var(--td-brand-color); } } > p { margin-top: 8px; font-size: var(--app-text-sm); } }
.create-footer { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 10px; padding-top: 8px; }
.template-field, .scope-field { border: 0; margin: 0; padding: 0; min-width: 0; legend { font-size: var(--app-text-md); font-weight: 500; margin-bottom: 11px; } }
.template-options { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; button { padding: 14px; border: 1px solid var(--app-border-subtle); background: var(--app-surface-raised); border-radius: var(--app-radius-md); color: var(--td-text-color-primary); text-align: left; cursor: pointer; transition: border-color var(--app-motion-fast), background-color var(--app-motion-fast); &:hover:not(:disabled), &.selected { border-color: var(--td-text-color-link); } &.selected { background: var(--td-brand-color-light); } } }
.template-name { display: flex; align-items: center; gap: 7px; font-size: var(--app-text-md); font-weight: 500; .template-check { color: var(--td-text-color-link); margin-left: auto; flex-shrink: 0; } }
.template-outline { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; font-size: var(--app-text-xs); color: var(--td-text-color-secondary); margin-top: 9px; line-height: 1.7; }
.evidence-scope-options { display: flex; flex-direction: column; align-items: flex-start; gap: 12px; }
.scope-field .muted { margin-top: 13px; font-size: var(--app-text-sm); }
.diff-summary { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); margin: 4px 0 16px; > span:first-child { margin-right: auto; } }
.diff-count { padding: 4px 8px; border-radius: var(--app-radius-xs); background: var(--app-surface-muted); }
.added { color: var(--td-success-color-7); }
.removed, .danger { color: var(--td-error-color-8); }
.proposal-diff { border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-md); background: var(--app-surface-raised); max-height: 60vh; overflow: auto; font: var(--app-text-md)/1.9 var(--app-font-family-mono); white-space: pre-wrap; overflow-wrap: anywhere; margin: 0; padding: 12px 0; span { display: block; padding: 0 14px; min-height: 1.9em; } .add { background: color-mix(in srgb, var(--td-success-color) 10%, transparent); color: var(--td-success-color-7); } .del { background: color-mix(in srgb, var(--td-error-color) 9%, transparent); color: var(--td-error-color-8); } }
.diff-footer { display: flex; justify-content: flex-end; gap: 12px; padding-top: 20px; }
.conflict-box { padding: 16px 20px; background: var(--td-warning-color-1); border: 1px solid var(--td-warning-color-3); border-radius: var(--app-radius-md); margin-bottom: 16px; font-size: var(--app-text-md); strong { font-weight: 500; } .row { margin-top: 12px; flex-wrap: wrap; } }
.history-note { padding-bottom: 16px; }
.history-empty { display: flex; flex-direction: column; align-items: center; gap: 12px; padding: 36px; color: var(--td-text-color-secondary); font-size: var(--app-text-md); }
.revision-row { display: flex; align-items: center; gap: 12px; border-bottom: 1px solid var(--app-border-subtle); padding: 16px 0; &:last-child { border-bottom: 0; } }
.revision-marker { display: flex; align-items: center; justify-content: center; width: 34px; height: 34px; border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-md); color: var(--td-text-color-secondary); flex-shrink: 0; }
.revision-copy { flex: 1; min-width: 0; strong { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; font-size: var(--app-text-md); font-weight: 500; } time { display: block; margin-top: 6px; font-size: var(--app-text-xs); color: var(--td-text-color-secondary); font-variant-numeric: tabular-nums; } }
.current-revision { color: var(--td-text-color-link); background: var(--td-brand-color-light); border-radius: var(--app-radius-xs); padding: 2px 5px; font-size: var(--app-text-xs); }
@media (max-width: 1350px) { .draft-layout.has-draft { grid-template-columns: 180px minmax(0, 1fr); } .assistant-panel { grid-column: 1 / -1; border-top: 1px solid var(--app-border-subtle); border-left: 0; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px 24px; } .assistant-heading, .assistant-panel > .t-alert, .assistant-panel > .save-hint, .assistant-panel > .evidence-note { grid-column: 1 / -1; } .assistant-panel > .t-button { align-self: end; } .evidence-note { padding-top: 0; } .draft-toolbar { flex-wrap: wrap; } }
@media (max-width: 760px) { .draft-workspace { padding-top: 20px; } .draft-layout, .draft-layout.has-draft { display: flex; flex-direction: column; min-height: 480px; } .draft-list { border-right: 0; border-bottom: 1px solid var(--app-border-subtle); padding: 16px 12px 12px; } .list-label { padding-bottom: 10px; } .draft-list-items { display: flex; overflow-x: auto; gap: 8px; padding: 3px; } .draft-item { width: 180px; flex-shrink: 0; margin: 0; } .list-settings { margin-top: 10px; } .editor-area { padding: 20px; } .toolbar-controls { flex-wrap: wrap; width: 100%; } .draft-kb-select { flex: 1; min-width: 170px; } .assistant-panel { display: flex; padding: 22px 20px; } .editor-actions { gap: 12px; } .markdown-editor, .rendered-draft { min-height: 320px; } .draft-empty { min-height: 320px; padding: 32px 24px; } }
@media (max-width: 420px) { .editor-area { padding: 18px 14px; } .editor-actions .row { flex: 1; justify-content: flex-end; gap: 4px; } .editor-actions { row-gap: 8px; } .template-options { grid-template-columns: 1fr; } .export-row > span { flex-basis: 100%; padding-bottom: 4px; } .diff-footer { flex-wrap: wrap; } }
@media (max-width: 1350px) { .assistant-panel > .writing-experience { grid-column: 1 / -1; } }
@media (pointer: coarse) { .view-switch button, .draft-workspace :deep(.t-button), .source-references button { min-height: 44px; } }
@media (prefers-reduced-motion: reduce) { .draft-item, .pending-list button, .template-options button { transition: none; } }
</style>
