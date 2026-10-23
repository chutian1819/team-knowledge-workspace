<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { listKnowledgeBases, listKnowledgeFiles, createKnowledgeFromURL, createManualKnowledge, uploadKnowledgeFile, updateKnowledgeMetadata, reparseKnowledge } from '@/api/knowledge-base'
import { listMyOrganizations, listMembers, listSharedKnowledgeBases, listOrganizationSharedKnowledgeBases, type Organization, type OrganizationMember } from '@/api/organization'
import { BUILTIN_QUICK_ANSWER_ID } from '@/api/agent'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import { MAX_FILE_SIZE_BYTES, MAX_FILE_SIZE_MB } from '@/utils'
import KnowledgeBaseEditorModal from '@/views/knowledge/KnowledgeBaseEditorModal.vue'
import ResearchDraftEditor from './ResearchDraftEditor.vue'
import * as api from '@/api/research'
import { sourceState as describeSourceState, quickNotePublishingReady, collectionSearchEnabled, errorMessage, jobLabel, terminalJob, exactMemberIds, chatSourceScope, quickNotePayload, sourceVersionFilename, isCurrentAudience, completedShareSince, type SourceItem } from './workspaceState'
import { collectSources, type ExistingSource } from './captureFlow'
import { personalSourceBases, sectionSourceBases, resolveWorkspaceLocation, collectionDisplayName, type WorkspaceSection } from './workspaceNavigation'
import { imageCaptureOverrides, imageRecognitionConfigured, isCaptureImage, type ImageCaptureMode } from './imageCapture'
const router = useRouter(); const route = useRoute(); const settings = useSettingsStore(); const auth = useAuthStore()
const capabilities = useDeploymentCapabilitiesStore()
const inboxId = ref('')
const knowledgeBases = ref<any[]>([]); const organizations = ref<Organization[]>([]); const sharedBases = ref<any[]>([])
const kbId = ref(typeof route.query.kb === 'string' ? route.query.kb : '')
const section = ref<WorkspaceSection>(route.query.section === 'team' ? 'team' : 'library'); const tab = ref<'sources' | 'drafts'>('sources'); const draftVisited = ref(false)
const lastSectionKb = new Map<WorkspaceSection, string>()
const sourceList = ref<SourceItem[]>([]); const selectedIds = ref<string[]>([])
const total = ref(0); const page = ref(1); const query = ref(''); const busy = ref(''); const error = ref(''); const loading = ref(false); const resourcesLoading = ref(false)
const captureMode = ref<'url' | 'file' | 'note'>('url'); const url = ref(''); const noteTitle = ref(''); const noteContent = ref(''); const provenance = ref('')
const existingSources = ref<ExistingSource[]>([])
const files = ref<File[]>([]); const uploadProgress = ref(0); const createKbVisible = ref(false); const dragging = ref(false)
const imageCaptureMode = ref<ImageCaptureMode>('recognize')
const selectedImageCount = computed(() => files.value.filter(isCaptureImage).length)
const bookmarkInput = ref<HTMLInputElement | null>(null); const fileInput = ref<HTMLInputElement | null>(null); const folderInput = ref<HTMLInputElement | null>(null)
const bookmarks = ref<api.BookmarkItem[]>([]); const skipped = ref(0); const bookmarkPreview = ref(false); const bookmarkName = ref(''); const bookmarkKey = ref('')
const captureJobs = ref<api.CaptureJob[]>([]); const showJobs = ref(false)
const membersLoading = ref(false); let audienceRequest = 0
const shareVisible = ref(false); const targetKb = ref(''); const organizationId = ref(''); const members = ref<OrganizationMember[]>([]); const confirmedAudience = ref(false); const shareKey = ref(''); const selectedProject = ref(typeof route.query.project === 'string' ? route.query.project : '')
const activeKb = computed(() => knowledgeBases.value.find(kb => kb.id === kbId.value) || sharedBases.value.find(item => item.knowledge_base.id === kbId.value)?.knowledge_base)
const allBases = computed(() => [...new Map([...sharedBases.value.map(item => ({ ...item.knowledge_base, research_permission: item.my_permission || item.permission })), ...knowledgeBases.value].map(kb => [kb.id, kb])).values()])
const nativeBases = computed(() => personalSourceBases(knowledgeBases.value))
const sectionBases = computed(() => sectionSourceBases(section.value, knowledgeBases.value, sharedBases.value, selectedProject.value, inboxId.value))
const canCreateCollection = computed(() => auth.hasRole('contributor'))
const canManageProjects = computed(() => auth.hasRole('admin') && capabilities.isSupported('organizations'))
const isOwn = computed(() => knowledgeBases.value.some(kb => kb.id === kbId.value))
const canWrite = computed(() => isOwn.value && auth.hasRole('contributor'))
const canDownloadOriginal = computed(() => auth.hasRole('contributor') && (isOwn.value || sharedBases.value.some(item => item.knowledge_base.id === kbId.value && ['admin', 'editor'].includes(item.my_permission || item.permission))))
const inbox = computed(() => nativeBases.value.find(kb => kb.id === inboxId.value))
const shareTargets = computed(() => nativeBases.value.filter(kb => kb.id !== kbId.value && kb.id !== inbox.value?.id))
const notesCanPublish = computed(() => quickNotePublishingReady(activeKb.value))
const searchEnabled = computed(() => collectionSearchEnabled(activeKb.value))
const canRecognizeImages = computed(() => imageRecognitionConfigured(activeKb.value))
const imageUploadBlocked = computed(() => selectedImageCount.value > 0 && imageCaptureMode.value === 'recognize' && !canRecognizeImages.value)
const selectionReady = computed(() => selectedIds.value.length > 0 && selectedIds.value.every(id => sourceList.value.some(item => item.id === id && sourceState(item).tone === 'success')))
const selectedArchivedImages = computed(() => selectedIds.value.some(id => sourceList.value.some(item => item.id === id && item.parse_status === 'archived')))
const allSelected = computed(() => sourceList.value.length > 0 && sourceList.value.every(item => selectedIds.value.includes(item.id)))
const draftEditor = ref<InstanceType<typeof ResearchDraftEditor> | null>(null)
const assetJobs = ref<api.ExportJob[]>([])
const historyItem = ref<SourceItem | null>(null)
const sourceVersions = ref<api.SourceVersion[]>([])
const historyError = ref(''); const versionDownload = ref('')
const replacementInput = ref<HTMLInputElement | null>(null); const replacementFile = ref<File | null>(null); const replacementProgress = ref(0)
const canReplaceSource = computed(() => canWrite.value && activeKb.value?.creator_id === auth.currentUserId && historyItem.value?.type === 'file' && !activeKb.value?.wiki_config?.research_only)
let historyRequest = 0
const liveJobs = computed(() => captureJobs.value.filter(job => !terminalJob(job.status)))
const pageLoading = computed(() => loading.value || resourcesLoading.value)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / 30)))
const workspaceSections = computed(() => ([{ id: 'library', icon: 'folder', title: '我的资料' }, { id: 'team', icon: 'usergroup', title: '团队项目' }] as const).filter(item => item.id !== 'team' || capabilities.isSupported('organizations')))
const sectionDescription = computed(() => section.value === 'library' ? '保存和整理资料，开展问答与写作。' : '查看团队共享的资料，协作完成研究。')
let pollTimer: ReturnType<typeof setTimeout> | undefined; let loadToken = 0; let disposed = false; let initialSectionResolved = false
function fail(err: unknown) { error.value = errorMessage(err) }
function sourceState(item: SourceItem) { return describeSourceState(item, searchEnabled.value) }
function displayName(item: SourceItem) { return item.title || item.file_name || item.source || '未命名资料' }
function dateLabel(date?: string) { if (!date) return ''; const parsed = new Date(date); return Number.isNaN(parsed.valueOf()) ? '' : parsed.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' }) }
function fileSizeLabel(size: number) { return size >= 1024 * 1024 ? `${(size / 1024 / 1024).toFixed(1)} MB` : `${Math.max(1, Math.round(size / 1024))} KB` }
function searchSources() { page.value = 1; void loadSources() }
function clearSearch() { query.value = ''; searchSources() }
function captureTabKey(event: KeyboardEvent) {
  const modes = ['url', 'file', 'note'] as const
  const index = modes.indexOf(captureMode.value)
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key) || busy.value) return
  event.preventDefault()
  captureMode.value = modes[event.key === 'Home' ? 0 : event.key === 'End' ? modes.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + modes.length) % modes.length]
  document.getElementById(`capture-tab-${captureMode.value}`)?.focus()
}
function contentTabKey(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  tab.value = event.key === 'Home' ? 'sources' : event.key === 'End' ? 'drafts' : tab.value === 'sources' ? 'drafts' : 'sources'
  document.getElementById(`workspace-tab-${tab.value}`)?.focus()
}
async function loadResources() {
  error.value = ''; resourcesLoading.value = true
  const results = await Promise.allSettled([listKnowledgeBases(), listMyOrganizations(), listSharedKnowledgeBases(), api.getInbox()])
  if (disposed) return
  if (results[0].status === 'fulfilled') knowledgeBases.value = results[0].value.data || []; else fail(results[0].reason)
  if (results[1].status === 'fulfilled') organizations.value = results[1].value.data?.organizations || []
  if (results[2].status === 'fulfilled') sharedBases.value = results[2].value.data || []; else fail(results[2].reason)
  inboxId.value = results[3].status === 'fulfilled' ? results[3].value.data?.id || '' : ''
  if (results[3].status === 'rejected') fail(results[3].reason)
  if (organizations.value.length) {
    const projects = await Promise.allSettled(organizations.value.map(org => listOrganizationSharedKnowledgeBases(org.id)))
    if (disposed) return
    const found = projects.flatMap(result => result.status === 'fulfilled' && result.value.success ? result.value.data || [] : [])
    sharedBases.value = [...new Map([...sharedBases.value, ...found].map(item => [`${item.organization_id}:${item.knowledge_base.id}`, item])).values()]
  }
  const location = resolveWorkspaceLocation(initialSectionResolved ? { section: section.value, kb: kbId.value, project: selectedProject.value } : route.query, knowledgeBases.value, sharedBases.value, inboxId.value)
  initialSectionResolved = true
  section.value = location.section; selectedProject.value = location.project; kbId.value = location.kb
  resourcesLoading.value = false
  syncLocation()
}
async function loadSources(quiet = false) {
  const kb = kbId.value; const token = ++loadToken
  if (!kb) { sourceList.value = []; total.value = 0; loading.value = false; return }
  if (!quiet) loading.value = true
  try {
    const result = await listKnowledgeFiles(kb, { page: page.value, page_size: 30, keyword: query.value || undefined, sort_by: 'created_at', sort_order: 'desc' })
    if (token !== loadToken || disposed) return
    sourceList.value = result.data || []; total.value = result.total || 0
    selectedIds.value = selectedIds.value.filter(id => sourceList.value.some(item => item.id === id))
  } catch (err) {
    if (token === loadToken) {
      const status = (err as { status?: number; $httpStatus?: number })?.$httpStatus || (err as { status?: number })?.status
      if (status === 403 || status === 404) {
        sourceList.value = []; total.value = 0; selectedIds.value = []
        historyRequest++; historyItem.value = null; sourceVersions.value = []; replacementFile.value = null
      }
      fail(err)
    }
  } finally { if (token === loadToken) loading.value = false }
}
async function loadAssetJobs() {
  const kb = kbId.value
  if (!kb || !isOwn.value) { assetJobs.value = []; return }
  try { const result = await api.listExports(kb); if (kb === kbId.value && !disposed) assetJobs.value = (result.data || []).filter(job => job.format === 'zip') }
  catch (err) { if (kb === kbId.value) fail(err) }
}
async function loadJobs() {
  if (!kbId.value || !isOwn.value) { captureJobs.value = []; return }
  const kb = kbId.value
  try { const result = await api.listCaptureJobs(kb); if (kb === kbId.value && !disposed) { const refreshProjects = completedShareSince(captureJobs.value, result.data || []); captureJobs.value = result.data || []; if (refreshProjects) await loadResources() } }
  catch (err) { if (kb === kbId.value) fail(err) }
}
async function tick() {
  if (disposed) return
  if (!document.hidden) {
    const processing = sourceList.value.some(item => ['pending', 'queued', 'processing', 'running', 'parsing'].includes(item.parse_status || ''))
    const hadJobs = liveJobs.value.length > 0
    // Other members' uploads and replacements do not create jobs in this browser.
    const sharedView = tab.value === 'sources' && !!kbId.value && (section.value === 'team' || !isOwn.value) && !busy.value
    if (assetJobs.value.some(job => !terminalJob(job.status))) await refreshAssets()
    if (hadJobs) await loadJobs()
    if (processing || hadJobs || sharedView) {
      await loadSources(true)
      if (historyItem.value && !busy.value) await sourceHistory(sourceList.value.find(item => item.id === historyItem.value?.id) || historyItem.value, true)
    }
  }
  if (!disposed) pollTimer = setTimeout(tick, 5000)
}
async function setupInbox() {
  if (!canCreateCollection.value || busy.value) return
  busy.value = 'inbox'; error.value = ''
  try { const response = await api.ensureInbox(); await loadResources(); kbId.value = response.data.id; section.value = 'library'; syncLocation(); await loadSources() }
  catch (err) { fail(err) } finally { busy.value = '' }
}
function changeSection(value: WorkspaceSection) {
  if (value === section.value) return
  if (busy.value || draftEditor.value?.busy) return
  if (draftEditor.value?.dirty && !window.confirm('研究稿有未保存修改，切换后将丢弃。是否继续？')) return
  initialSectionResolved = true
  lastSectionKb.set(section.value, kbId.value)
  const location = resolveWorkspaceLocation({ section: value, kb: lastSectionKb.get(value), project: selectedProject.value }, knowledgeBases.value, sharedBases.value, inboxId.value)
  section.value = value
  kbId.value = location.kb
  syncLocation()
}
function chooseProject(id: string) { if (busy.value || draftEditor.value?.busy) return; if (draftEditor.value?.dirty && !window.confirm('研究稿有未保存修改，切换后将丢弃。是否继续？')) return; selectedProject.value = selectedProject.value === id ? '' : id; kbId.value = sectionBases.value[0]?.id || ''; syncLocation() }
function syncLocation() {
  if (!initialSectionResolved || route.name !== 'researchWorkspace') return
  if (route.query.section === section.value && (route.query.kb || '') === kbId.value && (route.query.project || '') === selectedProject.value) return
  void router.replace({ query: { ...route.query, section: section.value, kb: kbId.value || undefined, project: selectedProject.value || undefined } })
}
function acceptFiles(selected: File[]) {
  if (busy.value || !selected.length) return
  const oversized = selected.filter(file => file.size > MAX_FILE_SIZE_BYTES)
  files.value = selected.filter(file => file.size <= MAX_FILE_SIZE_BYTES)
  imageCaptureMode.value = 'recognize'
  error.value = oversized.length ? `${oversized.length} 个文件超过 ${MAX_FILE_SIZE_MB} MB，已从待上传列表中移除。` : ''
  captureMode.value = 'file'
}
function selectedFiles(event: Event) { const input = event.target as HTMLInputElement; acceptFiles(Array.from(input.files || [])); input.value = '' }
function dropped(event: DragEvent) { dragging.value = false; acceptFiles(Array.from(event.dataTransfer?.files || [])) }
async function attachProvenance(result: any) {
  const knowledgeId = result?.data?.id
  if (knowledgeId && provenance.value.trim()) {
    try { await updateKnowledgeMetadata(knowledgeId, { ...(result.data.custom_metadata || {}), research_source_person: provenance.value.trim(), research_captured_at: new Date().toISOString() }) }
    catch (err) { throw new Error(`资料已保存，但来源备注未写入：${errorMessage(err)}。请在原文详情中补充，勿重复上传。`) }
  }
}
async function capture() {
  if (!kbId.value || !canWrite.value || busy.value) return
  const destinationKbId = kbId.value
  const imageMode = imageCaptureMode.value
  const archivesImages = captureMode.value === 'file' && imageMode === 'archive' && selectedImageCount.value > 0
  let savedDraft = false
  let counts = { created: 0, existing: 0 }
  const rememberExisting = (source: ExistingSource) => {
    if (!existingSources.value.some(item => item.id === source.id)) existingSources.value.push(source)
  }
  busy.value = 'capture'; error.value = ''; uploadProgress.value = 0; existingSources.value = []
  try {
    if (captureMode.value === 'url') {
      const parsed = new URL(url.value.trim()); if (!['http:', 'https:'].includes(parsed.protocol)) throw new Error('请输入 http 或 https 网页链接。')
      counts = await collectSources([parsed.toString()], value => createKnowledgeFromURL(destinationKbId, { url: value }), {
        settled: () => { url.value = '' }, created: attachProvenance, existing: rememberExisting,
      })
    } else if (captureMode.value === 'note') {
      const payload = quickNotePayload(noteTitle.value, noteContent.value, notesCanPublish.value)
      const result = await createManualKnowledge(destinationKbId, payload)
      if (!result?.success || !result.data?.id) throw new Error('服务未确认笔记保存成功，输入内容已保留，请重试。')
      savedDraft = payload.status === 'draft'
      noteTitle.value = ''; noteContent.value = ''; await attachProvenance(result)
    } else {
      if (!files.value.length) throw new Error('请先选择文件。')
      // Validate the whole batch before uploading any document; archive consent
      // must be explicit and must not change midway through a mixed upload.
      files.value.forEach(file => imageCaptureOverrides(file, imageMode, canRecognizeImages.value))
      const remaining = [...files.value]
      counts = await collectSources([...files.value], file => uploadKnowledgeFile(destinationKbId, { file, fileName: (file as any).webkitRelativePath || file.name, process_config: imageCaptureOverrides(file, imageMode, canRecognizeImages.value) }, event => {
        uploadProgress.value = event.total ? Math.round(event.loaded * 100 / event.total) : 0
      }), {
        settled: () => { remaining.shift(); files.value = [...remaining] }, created: attachProvenance, existing: rememberExisting,
      })
    }
    MessagePlugin.success(counts.existing
      ? counts.created ? `新保存 ${counts.created} 份资料，${counts.existing} 份此前已保存，继续使用原资料。` : '此前已保存，继续使用原资料。'
      : savedDraft ? '笔记已保存为草稿。完成资料集设置后即可发布。' : archivesImages ? '资料已提交。图片仅保存原图，不识别文字或含义；文档正常处理。请在列表查看归档状态。' : '资料已保存，可在列表中查看处理状态。')
    provenance.value = ''; page.value = 1
  } catch (err) { fail(err) } finally { busy.value = ''; await loadSources() }
}
async function previewBookmarkFile(event: Event) {
  const input = event.target as HTMLInputElement; const file = input.files?.[0]; input.value = ''; if (!file) return
  if (file.size > 6 * 1024 * 1024) { error.value = '书签文件超过 6 MB，请拆分后导入。'; return }
  busy.value = 'bookmarks'; error.value = ''
  try { const result = await api.previewBookmarks(await file.text()); bookmarks.value = result.data.items; skipped.value = result.data.skipped; bookmarkName.value = file.name; bookmarkKey.value = crypto.randomUUID(); bookmarkPreview.value = true }
  catch (err) { fail(err) } finally { busy.value = '' }
}
async function importBookmarks() {
  busy.value = 'import'; error.value = ''
  try { const result = await api.importBookmarks(kbId.value, bookmarks.value, bookmarkKey.value); captureJobs.value.unshift(result.data); showJobs.value = true; bookmarkPreview.value = false; MessagePlugin.success('已进入后台导入队列，可以继续工作。') }
  catch (err) { fail(err) } finally { busy.value = '' }
}
async function retryJob(job: api.CaptureJob) {
  busy.value = `retry:${job.id}`; error.value = ''
  try { await api.retryCaptureJob(kbId.value, job.id); await loadJobs() } catch (err) { fail(err) } finally { busy.value = '' }
}
function selectSource(id: string) { selectedIds.value = selectedIds.value.includes(id) ? selectedIds.value.filter(item => item !== id) : [...selectedIds.value, id] }
function toggleAll() { selectedIds.value = allSelected.value ? [] : sourceList.value.map(item => item.id) }
function openSource(item: SourceItem) { router.push({ path: `/platform/knowledge-bases/${encodeURIComponent(item.knowledge_base_id || kbId.value)}`, query: { knowledge_id: item.id } }) }
function askSources() {
  if (!kbId.value || (selectedIds.value.length > 0 && !selectionReady.value)) return
  const scope = chatSourceScope(kbId.value, selectedIds.value)
  settings.selectAgent(BUILTIN_QUICK_ANSWER_ID)
  settings.selectKnowledgeBases(scope.knowledgeBases); settings.clearFiles(); settings.settings.selectedFileKbMap = scope.fileKbMap
  settings.settings.selectedTags = []; scope.files.forEach(id => settings.addFile(id)); router.push('/platform/creatChat')
}
async function retrySource(item: SourceItem) { busy.value = item.id; try { await reparseKnowledge(item.id); await loadSources() } catch (err) { fail(err) } finally { busy.value = '' } }
async function recognizeArchivedImage(item: SourceItem) {
  if (!canWrite.value || busy.value || item.parse_status !== 'archived') return
  if (!canRecognizeImages.value) { error.value = '请先在资料集设置中启用视觉模型，再识别图片内容。原图仍保留。'; return }
  busy.value = item.id; error.value = ''
  try { await reparseKnowledge(item.id, { process_config: { image_archive_only: false, enable_multimodel: true } }); MessagePlugin.success('已提交图片识别，原图仍保留。请查看处理状态。'); await loadSources() }
  catch (err) { fail(err) } finally { busy.value = '' }
}
function openShare() { if (!selectionReady.value) { error.value = '所选资料全部处理完成后即可共享。'; return } if (!shareTargets.value.some(kb => kb.id === targetKb.value)) targetKb.value = ''; shareVisible.value = true; shareKey.value = crypto.randomUUID(); confirmedAudience.value = false; if (!organizationId.value) organizationId.value = organizations.value[0]?.id || ''; else void fetchMembers() }
async function fetchMembers() {
  const request = ++audienceRequest; const orgId = organizationId.value
  members.value = []; confirmedAudience.value = false; membersLoading.value = true
  if (!orgId) { membersLoading.value = false; return }
  try { const result = await listMembers(orgId)
    if (!isCurrentAudience(request, audienceRequest, orgId, organizationId.value)) return
    if (!result.success) throw new Error(result.message || '无法加载项目成员，请重试。')
    members.value = result.data?.members || []
  } catch (err) { if (request === audienceRequest) fail(err) }
  finally { if (request === audienceRequest) membersLoading.value = false }
}
async function share() {
  if (!confirmedAudience.value || !targetKb.value || !members.value.length || membersLoading.value || busy.value) return
  busy.value = 'share'; error.value = ''
  try { const result = await api.shareSources(kbId.value, { target_kb_id: targetKb.value, organization_id: organizationId.value, selected_tenant_ids: exactMemberIds(members.value), knowledge_ids: [...selectedIds.value], idempotency_key: shareKey.value }); captureJobs.value.unshift(result.data); shareVisible.value = false; showJobs.value = true; selectedIds.value = []; MessagePlugin.success('共享任务已提交，可在处理记录中查看进度。') }
  catch (err) { fail(err) } finally { busy.value = '' }
}
async function sourceHistory(item: SourceItem, quiet = false) {
  if (busy.value === 'replace-source') return
  const request = ++historyRequest; const kb = kbId.value
  if (historyItem.value?.id !== item.id) replacementFile.value = null
  historyItem.value = item
  if (!quiet) { sourceVersions.value = []; historyError.value = ''; busy.value = 'history' }
  try {
    const result = await api.listSourceVersions(kb, item.id)
    if (request === historyRequest && historyItem.value?.id === item.id && kb === kbId.value) sourceVersions.value = result.data || []
  } catch (err) { if (request === historyRequest) historyError.value = errorMessage(err) }
  finally { if (!quiet && request === historyRequest) busy.value = '' }
}
function closeHistory() { if (busy.value !== 'replace-source') { historyItem.value = null; replacementFile.value = null } }
function selectReplacement(event: Event) {
  const input = event.target as HTMLInputElement; const file = input.files?.[0]; input.value = ''
  if (!file || !canReplaceSource.value || busy.value) return
  if (file.size > MAX_FILE_SIZE_BYTES) { historyError.value = `文件超过 ${MAX_FILE_SIZE_MB} MB，请选择更小的文件。`; return }
  replacementFile.value = file; replacementProgress.value = 0; historyError.value = ''
}
async function replaceOriginal() {
  const source = historyItem.value; const file = replacementFile.value; const kb = kbId.value
  if (!source || !file || !canReplaceSource.value || busy.value || versionDownload.value) return
  busy.value = 'replace-source'; historyError.value = ''; replacementProgress.value = 0
  try {
    const result = await api.replaceSourceFile(kb, source.id, file, event => {
      replacementProgress.value = event.total ? Math.round(event.loaded * 100 / event.total) : 0
    })
    if (!result.success || result.data?.id !== source.id) throw new Error('服务未确认原件更新，请刷新资料状态后重试。已选文件仍保留。')
    const updated = { ...source, ...result.data }
    historyItem.value = updated; replacementFile.value = null
    MessagePlugin.success('原件已更新，历史版本已保留。')
    busy.value = ''
    await Promise.all([loadSources(true), sourceHistory(updated)])
  } catch (err) { historyError.value = errorMessage(err) }
  finally { busy.value = '' }
}
async function downloadVersion(version: api.SourceVersion, format: api.SourceVersionAssetKind) {
  const available = format === 'snapshot' ? version.snapshot_path : format === 'original' ? version.has_original : version.markdown_path
  if (!available || versionDownload.value || (format === 'original' && !canDownloadOriginal.value)) return
  versionDownload.value = `${version.id}:${format}`; historyError.value = ''
  try { api.downloadBlob(await api.downloadSourceVersionFile(kbId.value, version.knowledge_id, version.id, format), sourceVersionFilename(version, format, historyItem.value ? displayName(historyItem.value) : '来源')) }
  catch (err) { historyError.value = errorMessage(err) }
  finally { versionDownload.value = '' }
}
async function exportAssets() {
  busy.value = 'assets'; error.value = ''
  try { assetJobs.value.unshift((await api.exportProjectAssets(kbId.value)).data); MessagePlugin.success('项目归档已进入后台队列') }
  catch (err) { fail(err) } finally { busy.value = '' }
}
async function refreshAssets() {
  const kb = kbId.value
  try { for (const job of assetJobs.value.filter(item => !terminalJob(item.status))) {
    const result = await api.getDraftExport(kb, job.id)
    if (kb !== kbId.value) return
    assetJobs.value = assetJobs.value.map(item => item.id === job.id ? result.data : item)
  } } catch (err) { fail(err) }
}
async function downloadAssets(job: api.ExportJob) {
  try { api.downloadBlob(await api.downloadDraftExport(kbId.value, job.id), job.file_name || `${activeKb.value?.name || '资料集'}-archive.zip`) }
  catch (err) { fail(err) }
}
async function createdKb(id: string) { createKbVisible.value = false; await loadResources(); if (shareVisible.value) targetKb.value = id; else { section.value = 'library'; kbId.value = id } }
async function refresh() { await loadResources(); await Promise.allSettled([loadSources(), loadJobs(), loadAssetJobs()]) }
watch(kbId, async () => { existingSources.value = []; sourceList.value = []; total.value = 0; selectedIds.value = []; page.value = 1; query.value = ''; captureJobs.value = []; assetJobs.value = []; historyRequest++; historyItem.value = null; sourceVersions.value = []; replacementFile.value = null; draftVisited.value = false; tab.value = 'sources'; error.value = ''; syncLocation(); await Promise.allSettled([loadSources(), loadJobs(), loadAssetJobs()]) })
watch(tab, value => { if (value === 'drafts') draftVisited.value = true })
watch(organizationId, fetchMembers)
watch([targetKb, organizationId, () => selectedIds.value.join(',')], () => { confirmedAudience.value = false; shareKey.value = crypto.randomUUID() })
onBeforeRouteUpdate(to => {
  const location = resolveWorkspaceLocation(to.query, knowledgeBases.value, sharedBases.value, inboxId.value)
  if (location.kb === kbId.value && location.section === section.value && location.project === selectedProject.value) return true
  if (busy.value || draftEditor.value?.busy) return false
  return !draftEditor.value?.dirty || window.confirm('研究稿有未保存修改，切换后将丢弃。是否继续？')
})
watch(() => [route.query.kb, route.query.section, route.query.project], () => {
  if (!initialSectionResolved || route.name !== 'researchWorkspace') return
  const location = resolveWorkspaceLocation(route.query, knowledgeBases.value, sharedBases.value, inboxId.value)
  section.value = location.section; selectedProject.value = location.project; kbId.value = location.kb
})
onMounted(async () => { await refresh(); pollTimer = setTimeout(tick, 5000) })
onBeforeUnmount(() => { disposed = true; loadToken++; if (pollTimer) clearTimeout(pollTimer) })
</script>

<template>
  <main class="research-shell">
    <header class="workspace-header">
      <div class="identity">
        <span class="identity-mark" aria-hidden="true"><t-icon name="layers" size="22px" /></span>
        <div><h1>研究工作台</h1><p>{{ sectionDescription }}</p></div>
      </div>

    </header>
    <nav class="workspace-nav" aria-label="工作区">
      <button v-for="item in workspaceSections" :key="item.id" type="button" :class="{ active: section === item.id }" :aria-pressed="section === item.id" :disabled="!!busy || !!draftEditor?.busy" @click="changeSection(item.id)">
        <t-icon :name="item.icon" aria-hidden="true" />{{ item.title }}
      </button>
    </nav>

    <div class="workspace-body">
      <t-alert v-if="error" class="page-alert" theme="error" :message="error" close @close="error = ''" />
      <section v-if="section === 'team'" class="team-section">
        <div class="section-heading"><div><h2>团队项目</h2><p>查看项目成员共享的资料。</p></div><t-button v-if="canManageProjects" variant="outline" @click="router.push('/platform/organizations')">管理项目<t-icon name="chevron-right" /></t-button></div>
        <div class="project-grid">
          <button v-for="project in organizations" :key="project.id" type="button" :class="{ active: selectedProject === project.id }" :aria-pressed="selectedProject === project.id" :disabled="!!busy || !!draftEditor?.busy" @click="chooseProject(project.id)">
            <span class="project-initial" aria-hidden="true">{{ project.name.slice(0, 1) }}</span><span class="project-copy"><strong>{{ project.name }}</strong><small>{{ project.member_count || 0 }} 位成员 · {{ sharedBases.filter(item => item.organization_id === project.id).length }} 个资料集</small></span><t-icon v-if="selectedProject === project.id" name="check-circle-filled" class="project-check" />
          </button>
          <div v-if="!organizations.length && !resourcesLoading" class="empty-project"><div><h3>暂无团队项目</h3><p>{{ canManageProjects ? '创建项目后，可邀请成员并共享资料。' : '加入项目后，即可查看成员共享的资料。' }}</p></div><t-button v-if="canManageProjects" variant="outline" @click="router.push('/platform/organizations')">创建项目</t-button></div>
          <t-loading v-if="resourcesLoading && !organizations.length" text="正在加载项目" />
        </div>
      </section>

      <div v-if="sectionBases.length" class="dataset-bar">
        <div class="dataset-title">
          <t-icon name="folder-open" size="20px" aria-hidden="true" />
          <t-select v-model="kbId" class="dataset-select" :disabled="!!busy || !!draftEditor?.busy || draftEditor?.dirty" :aria-label="section === 'team' ? '项目共享资料集' : '当前资料集'" :placeholder="section === 'team' ? '选择项目资料集' : '选择资料集'">
            <t-option v-for="kb in sectionBases" :key="kb.id" :value="kb.id" :label="collectionDisplayName(kb, inboxId)" />
          </t-select>
          <span v-if="activeKb" class="dataset-permission"><t-icon :name="isOwn ? 'lock-on' : 'usergroup'" />{{ isOwn ? '个人资料集' : '团队共享' }}</span>
        </div>
        <div class="row"><t-button v-if="canCreateCollection && section === 'library'" variant="text" :disabled="!!busy || !!draftEditor?.busy || draftEditor?.dirty" @click="createKbVisible = true"><t-icon name="add" />新建资料集</t-button><t-button variant="text" :loading="pageLoading" :disabled="!!busy || !!draftEditor?.busy" @click="refresh"><t-icon name="refresh" />刷新</t-button></div>
      </div>

      <template v-if="kbId">
        <div class="content-nav">
          <div class="content-tabs" role="tablist" aria-label="资料集内容" @keydown="contentTabKey">
            <button id="workspace-tab-sources" type="button" role="tab" aria-controls="workspace-panel-sources" :aria-selected="tab === 'sources'" :tabindex="tab === 'sources' ? 0 : -1" :class="{ active: tab === 'sources' }" @click="tab = 'sources'">资料<span>{{ total }}</span></button>
            <button id="workspace-tab-drafts" type="button" role="tab" aria-controls="workspace-panel-drafts" :aria-selected="tab === 'drafts'" :tabindex="tab === 'drafts' ? 0 : -1" :class="{ active: tab === 'drafts' }" @click="tab = 'drafts'">研究稿</button>
          </div>
          <div class="content-links"><t-button v-if="activeKb?.indexing_strategy?.wiki_enabled" variant="text" size="small" @click="router.push(`/platform/knowledge-bases/${kbId}?tab=graph`)">知识图谱<t-icon name="chevron-right" /></t-button><t-button variant="text" size="small" @click="router.push(`/platform/knowledge-bases/${kbId}`)">管理资料<t-icon name="chevron-right" /></t-button></div>
        </div>
        <div id="workspace-panel-sources" v-show="tab === 'sources'" role="tabpanel" aria-labelledby="workspace-tab-sources">
          <section v-if="canWrite" class="capture-card" :class="{ dragging }" aria-label="添加资料" @dragover.prevent="dragging = !busy" @dragleave.self="dragging = false" @drop.prevent="dropped">
            <div class="capture-heading">
              <div class="capture-modes" role="tablist" aria-label="添加资料的方式" @keydown="captureTabKey">
                <button v-for="mode in (['url', 'file', 'note'] as const)" :id="`capture-tab-${mode}`" :key="mode" type="button" role="tab" :aria-controls="`capture-panel-${mode}`" :aria-selected="captureMode === mode" :tabindex="captureMode === mode ? 0 : -1" :disabled="!!busy" :class="{ active: captureMode === mode }" @click="captureMode = mode"><t-icon :name="mode === 'url' ? 'link' : mode === 'file' ? 'upload' : 'edit'" aria-hidden="true" />{{ mode === 'url' ? '保存链接' : mode === 'file' ? '上传文件' : '新建笔记' }}</button>
              </div>
              <t-button variant="text" size="small" :disabled="!!busy" @click="bookmarkInput?.click()"><t-icon name="bookmark" />导入书签</t-button>
            </div>
            <div v-if="captureMode === 'url'" id="capture-panel-url" class="capture-main" role="tabpanel" aria-labelledby="capture-tab-url"><t-input v-model="url" placeholder="输入或粘贴网页链接" aria-label="网页链接" size="large" :disabled="!!busy" @enter="capture"><template #prefix-icon><t-icon name="link" /></template></t-input><t-button size="large" :loading="busy === 'capture'" :disabled="!url.trim() || !!busy" @click="capture">保存链接</t-button></div>
            <div v-else-if="captureMode === 'file'" id="capture-panel-file" class="file-capture" role="tabpanel" aria-labelledby="capture-tab-file">
              <div class="file-drop"><span class="upload-icon"><t-icon name="cloud-upload" size="26px" /></span><div><strong>{{ files.length ? `已选择 ${files.length} 个文件` : '拖放文件到此处' }}</strong><p>支持 PDF、Word、Markdown 和图片，单个文件不超过 {{ MAX_FILE_SIZE_MB }} MB</p></div><div class="row"><t-button variant="outline" :disabled="!!busy" @click="fileInput?.click()">选择文件</t-button><t-button variant="text" :disabled="!!busy" @click="folderInput?.click()">选择文件夹</t-button></div></div>
              <ul v-if="files.length" class="selected-files" aria-label="待上传文件"><li v-for="(file, index) in files" :key="`${file.name}-${index}`"><t-icon name="file" aria-hidden="true" /><span :title="file.name">{{ file.name }}</span><small>{{ fileSizeLabel(file.size) }}</small><t-button variant="text" size="small" shape="square" :disabled="!!busy" :aria-label="`移除 ${file.name}`" @click="files.splice(index, 1)"><t-icon name="close" /></t-button></li></ul>
              <div v-if="selectedImageCount" class="note-form">
                <strong>图片处理方式 · {{ selectedImageCount }} 张</strong>
                <t-radio-group v-model="imageCaptureMode" aria-label="本次上传的图片处理方式" :disabled="!!busy"><t-radio value="recognize">识别图片内容</t-radio><t-radio value="archive">仅保存原图</t-radio></t-radio-group>
                <p v-if="imageCaptureMode === 'archive'" class="muted" role="status">仅归档图片原件，不识别文字或画面内容，不能作为已识别的文字证据用于写作。PDF 等文档仍正常解析。</p>
                <p v-else-if="!canRecognizeImages" class="muted" role="status">当前资料集未启用视觉模型。请先配置视觉模型，或主动选择“仅保存原图”后上传。<t-link @click="router.push(`/platform/knowledge-bases/${kbId}`)">配置视觉识别</t-link></p>
                <p v-else class="muted">使用当前资料集配置的视觉模型识别图片内容，处理结果会显示在资料列表。</p>
              </div>
              <div v-if="files.length" class="file-submit"><span class="muted" role="status">{{ busy === 'capture' ? `正在上传 ${uploadProgress}%` : `${files.length} 个文件待上传` }}</span><t-button :disabled="!!busy || imageUploadBlocked" :loading="busy === 'capture'" @click="capture">上传文件</t-button></div>
            </div>
            <div v-else id="capture-panel-note" class="note-form" role="tabpanel" aria-labelledby="capture-tab-note"><t-input :disabled="!!busy" v-model="noteTitle" placeholder="笔记标题" aria-label="笔记标题" /><t-textarea :disabled="!!busy" v-model="noteContent" placeholder="输入笔记内容" aria-label="笔记内容" :autosize="{ minRows: 4, maxRows: 10 }" /><t-button :disabled="!noteTitle.trim() || !noteContent.trim() || !!busy" :loading="busy === 'capture'" @click="capture">保存笔记</t-button></div>
            <div class="capture-foot"><t-input :disabled="!!busy" v-model="provenance" placeholder="来源备注（选填）" aria-label="来源备注" size="small" /><span><t-icon name="folder" aria-hidden="true" />保存至 {{ activeKb ? collectionDisplayName(activeKb, inboxId) : '当前资料集' }}</span></div>
            <input ref="fileInput" type="file" multiple hidden @change="selectedFiles" /><input ref="folderInput" type="file" multiple webkitdirectory hidden @change="selectedFiles" /><input ref="bookmarkInput" type="file" accept=".html,.htm,text/html" hidden @change="previewBookmarkFile" />
          </section>
          <t-alert v-if="existingSources.length" theme="info" class="existing-capture" close @close="existingSources = []"><template #message><strong>资料已存在</strong><p>已保留原资料及来源备注。</p><div class="existing-links"><t-link v-for="source in existingSources" :key="source.id" :disabled="!!busy" @click="openSource(source)">{{ displayName(source) }}<t-icon name="chevron-right" /></t-link></div></template></t-alert>
          <t-alert v-if="isOwn && !notesCanPublish" theme="warning" class="model-alert"><template #message>当前检索方式需要 Embedding 模型，笔记暂存为草稿。配置模型或关闭检索索引后可发布。<t-link @click="router.push(`/platform/knowledge-bases/${kbId}`)">前往设置</t-link></template></t-alert>

          <t-alert v-if="activeKb && !searchEnabled" theme="info" class="model-alert" message="当前资料集未启用检索索引。仍可保存、解析资料，并选择资料撰写研究稿；整库检索问答需先启用索引。" />
          <div class="sources-toolbar"><div class="sources-title"><h2>全部资料</h2><span class="muted">{{ total }} 份</span></div><div class="source-controls"><t-input v-model="query" class="source-search" placeholder="搜索资料名称" aria-label="搜索资料" clearable @enter="searchSources" @clear="searchSources"><template #suffix-icon><button type="button" class="search-button" aria-label="搜索" @click="searchSources"><t-icon name="search" /></button></template></t-input><t-button variant="text" :aria-expanded="showJobs" aria-controls="capture-jobs" @click="showJobs = !showJobs"><t-icon name="time" />处理记录<span v-if="liveJobs.length" class="job-count">{{ liveJobs.length }}</span></t-button></div></div>
          <section v-if="showJobs" id="capture-jobs" class="jobs-panel"><div class="panel-title"><h3>处理记录</h3><t-button size="small" variant="text" :disabled="!!busy" @click="loadJobs">刷新记录</t-button></div><p v-if="!captureJobs.length" class="muted">暂无书签导入或共享记录。</p><details v-for="job in captureJobs" :key="job.id"><summary><span>{{ job.kind === 'share' ? '项目共享' : '书签导入' }}</span><span>{{ jobLabel(job.status) }} · {{ job.completed }}/{{ job.total }}<span v-if="job.failed"> · {{ job.failed }} 项失败</span></span></summary><div v-for="(item, index) in job.items" :key="index" class="job-item"><span>{{ item.title || item.url || item.knowledge_id }}</span><span :class="{ danger: item.status === 'failed' }">{{ jobLabel(item.status) }}</span><small v-if="item.error">{{ item.error }}</small></div><t-button v-if="job.status === 'failed' || job.status === 'partial'" size="small" variant="outline" :disabled="!!busy" :loading="busy === `retry:${job.id}`" @click="retryJob(job)">重试失败项</t-button></details></section>
          <div v-if="selectedIds.length" class="selection-bar" aria-live="polite"><div><strong>已选择 {{ selectedIds.length }} 份</strong><span v-if="selectedArchivedImages" class="muted">已选原图尚未识别；请取消选择，或到原文详情配置视觉识别后再用于写作。</span><span v-else-if="!selectionReady" class="muted">部分资料尚未完成处理，请查看状态</span></div><div class="row"><t-button variant="outline" size="small" :disabled="!selectionReady" @click="askSources">资料问答</t-button><t-button v-if="canCreateCollection" variant="outline" size="small" :disabled="!selectionReady" @click="tab = 'drafts'">写研究稿</t-button><t-button v-if="canWrite && capabilities.isSupported('organizations')" size="small" :disabled="!!busy || !selectionReady" @click="openShare">共享给项目</t-button><t-button size="small" variant="text" @click="selectedIds = []">取消选择</t-button></div></div>

          <div class="source-table" :aria-busy="pageLoading">
            <div class="source-table-head"><t-checkbox :checked="allSelected" :disabled="!sourceList.length || loading" :indeterminate="selectedIds.length > 0 && !allSelected" aria-label="选择当前页所有资料" @change="toggleAll" /><span>名称与来源</span><span class="status-heading">处理状态</span><span class="date-heading">保存时间</span><span class="action-heading">操作</span></div>
            <div v-if="pageLoading && sourceList.length" class="list-loading" role="status"><t-loading size="small" />正在更新资料</div>
            <div v-for="item in sourceList" :key="item.id" class="source-row" :class="{ selected: selectedIds.includes(item.id) }">
              <t-checkbox class="source-checkbox" :checked="selectedIds.includes(item.id)" :disabled="loading" :aria-label="`选择 ${displayName(item)}`" @change="selectSource(item.id)" />
              <button type="button" class="source-main" :title="displayName(item)" @click="openSource(item)"><span class="source-icon" :class="item.type" aria-hidden="true"><t-icon :name="item.type === 'url' ? 'link' : item.type === 'manual' ? 'edit' : 'file'" size="20px" /></span><span class="source-copy"><strong>{{ displayName(item) }}</strong><small>{{ item.custom_metadata?.research_source_person || item.source || item.file_type || '原始资料' }}</small></span></button>
              <div class="source-status"><span class="state-pill" :class="sourceState(item).tone"><i aria-hidden="true" />{{ sourceState(item).label }}</span><p v-if="item.error_message" :class="item.parse_status === 'archived' ? 'muted' : 'source-error'" :title="item.error_message">{{ item.error_message }}</p></div>
              <time class="source-date" :datetime="item.created_at" :title="item.created_at ? new Date(item.created_at).toLocaleString('zh-CN') : ''">{{ dateLabel(item.created_at) || '—' }}</time>
              <div class="source-actions"><t-button v-if="canWrite && item.parse_status === 'archived'" size="small" variant="text" :disabled="!!busy" :loading="busy === item.id" @click="recognizeArchivedImage(item)">识别图片</t-button><t-button v-if="canWrite && sourceState(item).tone === 'danger'" size="small" variant="text" :disabled="!!busy" :loading="busy === item.id" :aria-label="`重试处理 ${displayName(item)}`" @click="retrySource(item)">重试</t-button><t-button v-else variant="text" size="small" :disabled="!!busy" :aria-label="`查看 ${displayName(item)} 的来源记录`" @click="sourceHistory(item)">来源记录</t-button></div>
            </div>
            <div v-if="!sourceList.length" class="source-empty" role="status"><t-loading v-if="pageLoading" text="正在加载资料" /><template v-else><span class="empty-icon"><t-icon :name="query ? 'search' : 'folder-open'" size="28px" /></span><h3>{{ query ? '未找到相关资料' : '暂无资料' }}</h3><p>{{ query ? '换一个关键词，或查看全部资料。' : canWrite ? '保存网页链接、上传文件或新建笔记。' : '项目共享的资料会显示在这里。' }}</p><t-button v-if="query" variant="outline" @click="clearSearch">清空搜索</t-button><t-button v-else-if="canWrite" variant="outline" @click="captureMode = 'file'; fileInput?.click()"><t-icon name="add" />上传文件</t-button></template></div>
          </div>
          <section v-if="assetJobs.length" class="jobs-panel"><div v-for="job in assetJobs" :key="job.id" class="asset-row"><span>资料集归档 · {{ jobLabel(job.status) }}</span><span v-if="job.error" class="danger">{{ job.error }}</span><t-button v-if="job.status === 'ready'" variant="text" @click="downloadAssets(job)">下载 ZIP</t-button><t-button v-else-if="job.status === 'failed'" variant="text" :disabled="!!busy" @click="exportAssets">重试</t-button><t-button v-else variant="text" @click="refreshAssets">刷新状态</t-button></div></section>
          <div class="list-footer"><div class="list-actions"><t-button variant="text" :disabled="!total || (selectedIds.length > 0 && !selectionReady)" @click="askSources"><t-icon name="chat" />{{ selectedIds.length ? '所选资料问答' : '资料集问答' }}</t-button><t-button v-if="isOwn" size="small" variant="text" :disabled="!!busy || !total" :loading="busy === 'assets'" @click="exportAssets"><t-icon name="download" />导出归档</t-button></div><div class="pagination" aria-label="资料分页"><span>第 {{ page }} / {{ pageCount }} 页</span><t-button size="small" variant="outline" :disabled="page <= 1 || loading" aria-label="上一页资料" @click="page--; loadSources()"><t-icon name="chevron-left" /></t-button><t-button size="small" variant="outline" :disabled="page >= pageCount || loading" aria-label="下一页资料" @click="page++; loadSources()"><t-icon name="chevron-right" /></t-button></div></div>
        </div>
        <div id="workspace-panel-drafts" v-show="tab === 'drafts'" role="tabpanel" aria-labelledby="workspace-tab-drafts"><ResearchDraftEditor v-if="draftVisited" ref="draftEditor" :key="kbId" :source-kb-id="kbId" :selected-ids="selectedIds" :knowledge-bases="allBases" @refresh="loadResources" /></div>
      </template>
      <section v-else class="source-empty empty-workspace" role="status"><t-loading v-if="pageLoading" text="正在加载资料集" /><template v-else><span class="empty-icon"><t-icon name="folder" size="28px" /></span><h3>{{ section === 'team' ? '暂无共享资料集' : '暂无资料集' }}</h3><p>{{ section === 'team' ? '项目成员共享资料后，即可在此查看。' : '保存网页、文件和笔记，之后可按主题整理。' }}</p><t-button v-if="section !== 'team' && canCreateCollection" :loading="busy === 'inbox'" :disabled="!!busy" @click="setupInbox">开始添加资料</t-button></template></section>
    </div>

    <t-dialog v-model:visible="bookmarkPreview" header="导入浏览器书签" width="min(720px, calc(100vw - 32px))" :confirm-btn="{ content: `导入 ${bookmarks.length} 条`, loading: busy === 'import', disabled: !bookmarks.length || !!busy }" @confirm="importBookmarks"><p class="muted">{{ bookmarkName }} · {{ bookmarks.length }} 条可导入，{{ skipped }} 条已过滤。目录和收藏时间将保留，重复链接不会再次保存。</p><div class="bookmark-list"><div v-for="(item, index) in bookmarks.slice(0, 100)" :key="index"><strong>{{ item.title || item.url }}</strong><span>{{ item.folder || '未分类' }}</span><small>{{ item.url }}</small></div></div><p v-if="bookmarks.length > 100" class="muted">当前展示前 100 条，全部 {{ bookmarks.length }} 条将被导入。</p></t-dialog>
    <t-dialog v-model:visible="shareVisible" header="共享资料到项目" width="min(620px, calc(100vw - 32px))" :confirm-btn="{ content: '确认共享', loading: busy === 'share', disabled: !confirmedAudience || !targetKb || !members.length || membersLoading || !!busy }" @confirm="share">
      <div class="share-form"><t-alert v-if="error" theme="error" :message="error" /><p class="muted">{{ selectedIds.length }} 份资料将移动到目标资料集。共享后，项目成员可查看这些资料及后续更新。</p><label>接收项目<t-select :disabled="!!busy" v-model="organizationId" aria-label="接收项目" placeholder="选择项目"><t-option v-for="org in organizations" :key="org.id" :value="org.id" :label="org.name" /></t-select></label><label>目标资料集<t-select :disabled="!!busy" v-model="targetKb" aria-label="目标资料集" placeholder="选择共享资料集"><t-option v-for="kb in shareTargets" :key="kb.id" :value="kb.id" :label="collectionDisplayName(kb, inboxId)" /></t-select></label><t-button variant="text" @click="createKbVisible = true"><t-icon name="add" />新建共享资料集</t-button><div class="audience"><strong>可访问的项目成员</strong><t-loading v-if="membersLoading" text="正在加载成员" /><div v-for="member in members" :key="member.id" class="audience-member"><span class="member-avatar">{{ (member.username || member.tenant_name || '?').slice(0, 1) }}</span><span>{{ member.username || member.tenant_name }}<small>{{ member.tenant_name }}</small></span><span class="muted">只读</span></div><p v-if="!members.length && !membersLoading" class="muted">{{ organizationId ? '暂无可共享成员，请检查项目成员设置。' : '选择项目后显示成员名单。' }}</p></div><t-checkbox :disabled="membersLoading || !members.length || !!busy" v-model="confirmedAudience">确认共享给以上全部成员</t-checkbox><p class="muted">如需仅向部分成员共享，请创建只包含这些成员的项目。</p><t-button v-if="canManageProjects" variant="text" @click="router.push('/platform/organizations')">管理项目成员<t-icon name="chevron-right" /></t-button></div>
    </t-dialog>
    <t-dialog :visible="!!historyItem" :header="historyItem ? `来源记录 · ${displayName(historyItem)}` : '来源记录'" width="min(680px, calc(100vw - 32px))" :footer="false" :close-btn="busy !== 'replace-source'" :close-on-overlay-click="busy !== 'replace-source'" :close-on-esc-keydown="busy !== 'replace-source'" @close="closeHistory">
      <div class="source-history"><p class="muted">查看或下载网页与文件的历史版本。</p><p v-if="!canDownloadOriginal" class="muted">当前可查看版本信息、正文和网页快照。下载历史原件需要贡献者角色和资料集编辑权限。</p><t-alert v-if="historyError" theme="error" :message="historyError" />
        <div v-if="canReplaceSource" class="replace-original"><input ref="replacementInput" type="file" hidden @change="selectReplacement" /><div><strong>更新原件</strong><p>历史版本与来源信息将保留，已共享的成员可查看更新后的内容。</p></div><div class="row"><t-button variant="outline" :disabled="!!busy || !!versionDownload" @click="replacementInput?.click()">{{ replacementFile ? '重新选择' : '选择新原件' }}</t-button><span v-if="replacementFile" class="replacement-name" :title="replacementFile.name">{{ replacementFile.name }}</span></div><div v-if="replacementFile" class="row"><t-button :disabled="!!versionDownload || (!!busy && busy !== 'replace-source')" :loading="busy === 'replace-source'" @click="replaceOriginal">{{ busy === 'replace-source' ? `正在更新 ${replacementProgress}%` : '确认更新' }}</t-button><t-button variant="text" :disabled="!!busy" @click="replacementFile = null">取消</t-button></div></div>
        <t-loading v-if="busy === 'history'" text="正在加载版本记录" /><p v-else-if="!sourceVersions.length && !historyError" class="muted">暂无历史版本。资料处理完成后，可刷新查看。</p>
        <div v-for="version in sourceVersions" :key="version.id" class="version-card"><div class="version-heading"><strong>{{ new Date(version.captured_at).toLocaleString('zh-CN') }}</strong><span v-if="version.file_type">{{ version.file_type.toUpperCase() }}</span></div><p v-if="version.file_name" class="version-filename">{{ version.file_name }}</p><p v-if="version.source_url">{{ version.source_url }}</p><div v-if="version.markdown_path || version.snapshot_path || (version.has_original && canDownloadOriginal)" class="row"><t-button v-if="version.has_original && canDownloadOriginal" size="small" variant="outline" :disabled="!!versionDownload" :loading="versionDownload === `${version.id}:original`" @click="downloadVersion(version, 'original')">下载原件</t-button><t-button v-if="version.snapshot_path" size="small" variant="outline" :disabled="!!versionDownload" :loading="versionDownload === `${version.id}:snapshot`" @click="downloadVersion(version, 'snapshot')">下载网页快照</t-button><t-button v-if="version.markdown_path" size="small" variant="outline" :disabled="!!versionDownload" :loading="versionDownload === `${version.id}:markdown`" @click="downloadVersion(version, 'markdown')">下载 Markdown</t-button></div></div>
        <div class="row history-actions"><t-button v-if="historyItem" variant="outline" :disabled="busy === 'replace-source'" @click="openSource(historyItem)">查看当前原文</t-button><t-button v-if="historyItem" variant="text" :disabled="!!versionDownload || busy === 'replace-source'" :loading="busy === 'history'" @click="sourceHistory(historyItem)">刷新记录</t-button><t-button v-if="canWrite && historyItem && historyItem.type === 'url'" :disabled="!!busy" :loading="busy === historyItem.id" @click="retrySource(historyItem)">重新获取网页</t-button></div>
      </div>
    </t-dialog>
    <KnowledgeBaseEditorModal :visible="createKbVisible" mode="create" initial-type="document" @update:visible="createKbVisible = $event" @success="createdKb" />
  </main>
</template>

<style scoped lang="less">
.research-shell {
  height: 100%; overflow: auto; background: var(--td-bg-color-page); color: var(--td-text-color-primary); scroll-padding-top: 74px;
  h1, h2, h3, p { margin: 0; }
  button { font-family: inherit; }
  button:focus-visible { outline: 2px solid var(--td-text-color-link); outline-offset: 3px; }
  button:disabled { cursor: not-allowed; }
}
.workspace-header { display: flex; align-items: center; justify-content: space-between; gap: 24px; padding: 36px 44px 28px; background: var(--td-bg-color-page); }
.identity { display: flex; align-items: center; gap: 14px; min-width: 0;
  h1 { font-size: var(--app-text-display); font-weight: 600; letter-spacing: -.04em; line-height: 1.35; }
  p { margin-top: 7px; font-size: var(--app-text-md); line-height: 1.7; color: var(--td-text-color-secondary); }
}
.identity-mark { width: 48px; height: 48px; flex: none; display: grid; place-items: center; background: var(--td-brand-color); color: var(--td-text-color-anti); border: 1px solid color-mix(in srgb, var(--td-brand-color) 80%, var(--td-text-color-primary)); border-radius: var(--app-radius-panel); box-shadow: inset 0 1px 0 rgba(255, 255, 255, .14), 0 4px 12px color-mix(in srgb, var(--td-brand-color) 12%, transparent); }
.header-actions, .row { display: flex; align-items: center; gap: 10px; }
.header-actions :deep(.t-button__text), .row :deep(.t-button__text), .list-actions :deep(.t-button__text), .content-links :deep(.t-button__text), .source-controls :deep(.t-button__text) { display: inline-flex; align-items: center; gap: 6px; }
.workspace-nav { position: sticky; top: 0; z-index: var(--z-sticky); display: flex; align-items: center; gap: 8px; padding: 10px 44px; background: var(--app-surface-glass-muted); border-top: 1px solid var(--app-border-highlight); border-bottom: 1px solid var(--app-border-subtle); backdrop-filter: blur(18px) saturate(120%); -webkit-backdrop-filter: blur(18px) saturate(120%);
  button { display: flex; align-items: center; gap: 8px; min-height: 38px; padding: 8px 14px; border: 1px solid transparent; border-radius: var(--app-radius-md); background: transparent; color: var(--td-text-color-secondary); font-size: var(--app-text-base); cursor: pointer; transition: color var(--app-motion-fast), background-color var(--app-motion-fast), border-color var(--app-motion-fast), box-shadow var(--app-motion-fast);
    &:hover { color: var(--td-text-color-primary); background: var(--td-bg-color-container-hover); }
    &.active { color: var(--td-text-color-brand); background: var(--app-surface-raised); border-color: var(--app-border-subtle); box-shadow: var(--app-shadow-sm); font-weight: 600; }
  }
}
.workspace-body { max-width: 1460px; margin: 0 auto; padding: 32px 44px 44px; }
.page-alert { margin-bottom: 20px; }
.setup-card { display: flex; align-items: center; gap: 20px; padding: 30px; min-height: 150px; background: var(--app-surface-raised); border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-xl); box-shadow: var(--app-shadow-sm);
  > div:not(.t-loading) { flex: 1; }
  h2 { font-size: var(--app-text-2xl); font-weight: 600; }
  p { margin-top: 8px; color: var(--td-text-color-secondary); font-size: var(--app-text-base); line-height: 1.7; }
}
.setup-icon, .empty-icon { display: grid; place-items: center; width: 56px; height: 56px; flex: none; color: var(--td-text-color-brand); background: var(--td-brand-color-light); border-radius: var(--app-radius-xl); }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-bottom: 18px;
  h2 { font-size: var(--app-text-2xl); font-weight: 600; }
  p { color: var(--td-text-color-secondary); font-size: var(--app-text-md); margin-top: 6px; }
}
.project-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 12px; margin-bottom: 28px;
  > button { display: flex; align-items: center; gap: 12px; text-align: left; padding: 18px; border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-xl); background: var(--app-surface-raised); color: inherit; cursor: pointer; transition: border-color var(--app-motion-fast), box-shadow var(--app-motion-fast);
    &:hover { border-color: var(--td-brand-color-focus); box-shadow: var(--app-shadow-sm); }
    &.active { border-color: var(--td-brand-color); background: color-mix(in srgb, var(--td-brand-color) 3%, var(--app-surface-raised)); }
  }
}
.project-initial { width: 38px; height: 38px; flex: none; display: grid; place-items: center; background: var(--td-brand-color-light); color: var(--td-text-color-brand); border-radius: var(--app-radius-lg); font-size: var(--app-text-2xl); }
.project-copy { min-width: 0; flex: 1;
  strong, small { display: block; }
  strong { font-size: var(--app-text-base); font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  small { margin-top: 7px; font-size: var(--app-text-sm); color: var(--td-text-color-secondary); }
}
.project-check { color: var(--td-text-color-brand); flex: none; }
.empty-project { grid-column: 1 / -1; display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 24px; background: var(--app-surface-raised); border: 1px dashed var(--td-component-border); border-radius: var(--app-radius-xl);
  h3 { font-size: var(--app-text-base); font-weight: 500; }
  p { margin-top: 6px; font-size: var(--app-text-md); color: var(--td-text-color-secondary); }
}
.dataset-bar { display: flex; justify-content: space-between; align-items: center; gap: 20px; margin-bottom: 12px; }
.dataset-title { display: flex; align-items: center; gap: 10px; min-width: 0; > .t-icon { color: var(--td-text-color-secondary); flex: none; } }
.dataset-select { width: 260px; max-width: 100%; }
.dataset-permission { display: inline-flex; align-items: center; gap: 5px; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); white-space: nowrap; }
.content-nav { display: flex; align-items: center; justify-content: space-between; gap: 20px; border-bottom: 1px solid var(--app-border-subtle); margin-bottom: 24px; }
.content-tabs { display: flex; align-items: center; gap: 28px;
  button { display: flex; align-items: center; gap: 7px; min-height: 48px; padding: 13px 0; border: 0; border-bottom: 2px solid transparent; background: none; color: var(--td-text-color-secondary); font-size: var(--app-text-base); cursor: pointer;
    &.active { color: var(--td-text-color-primary); font-weight: 600; border-bottom-color: var(--td-brand-color); }
    span { min-width: 22px; padding: 0 6px; background: var(--td-bg-color-component); border-radius: var(--app-radius-xs); color: var(--td-text-color-secondary); font-size: var(--app-text-sm); line-height: 20px; font-weight: 400; }
  }
}
.content-links { display: flex; align-items: center; gap: 4px; }
.capture-card { padding: 24px 26px 18px; margin-bottom: 24px; background: var(--app-surface-raised); border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-panel); box-shadow: var(--app-shadow-md), var(--app-shadow-inset); transition: border-color var(--app-motion-fast), background-color var(--app-motion-fast), box-shadow var(--app-motion-fast);
  &:focus-within { border-color: color-mix(in srgb, var(--td-text-color-link) 35%, var(--app-border-subtle)); }
  &.dragging { border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
}
.capture-heading { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 20px; }
.capture-modes { display: flex; gap: 3px; padding: 4px; background: var(--app-surface-muted); border-radius: var(--app-radius-lg);
  button { display: flex; align-items: center; justify-content: center; gap: 7px; padding: 7px 12px; min-height: 34px; border: 1px solid transparent; border-radius: var(--app-radius-sm); background: none; color: var(--td-text-color-secondary); font-size: var(--app-text-md); cursor: pointer; transition: background-color var(--app-motion-fast), color var(--app-motion-fast), box-shadow var(--app-motion-fast);
    &:hover { color: var(--td-text-color-primary); background: var(--td-bg-color-secondarycontainer-hover); }
    &.active { background: var(--app-surface-raised); border-color: var(--app-border-subtle); color: var(--td-text-color-brand); font-weight: 500; box-shadow: var(--app-shadow-sm); }
  }
}
.capture-main { display: flex; align-items: center; gap: 12px; > .t-input__wrap { min-width: 0; flex: 1; } > .t-button { flex: none; } }
.capture-foot { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-top: 16px; padding-top: 14px; border-top: 1px solid var(--app-border-subtle);
  > .t-input__wrap { max-width: 280px; }
  > span { display: flex; align-items: center; gap: 6px; min-width: 0; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
}
.file-drop { display: flex; align-items: center; gap: 16px; min-height: 100px; padding: 20px; background: var(--app-surface-muted); border: 1px dashed var(--td-component-border); border-radius: var(--app-radius-lg);
  > div:first-of-type { flex: 1; min-width: 0; }
  strong { font-size: var(--app-text-base); font-weight: 500; }
  p { margin-top: 6px; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); line-height: 1.7; }
}
.upload-icon { color: var(--td-text-color-brand); flex: none; }
.selected-files { list-style: none; max-height: 224px; overflow: auto; margin: 12px 0 0; padding: 0;
  li { display: flex; align-items: center; gap: 10px; padding: 6px 8px; border-bottom: 1px solid var(--app-border-subtle); font-size: var(--app-text-md); }
  li > span { flex: 1; min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  small { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); flex: none; }
}
.file-submit { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 14px; }
.note-form { display: grid; gap: 12px; > .t-button { justify-self: end; } }
.model-alert, .existing-capture { margin-bottom: 20px; }
.existing-capture { p { margin-top: 4px; font-size: var(--app-text-sm); } .existing-links { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 7px; } }
.sources-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin: 28px 0 16px; }
.sources-title { display: flex; align-items: baseline; gap: 10px; flex: none; h2 { font-size: var(--app-text-xl); font-weight: 600; } }
.source-controls { display: flex; align-items: center; gap: 12px; min-width: 0; }
.source-search { width: 240px; min-width: 0; }
.search-button { display: grid; place-items: center; width: 28px; height: 28px; border: 0; border-radius: var(--app-radius-xs); background: none; color: var(--td-text-color-secondary); cursor: pointer; &:hover { background: var(--td-bg-color-container-hover); color: var(--td-text-color-primary); } }
.job-count { padding: 0 5px; background: var(--td-brand-color-light); border-radius: var(--app-radius-xs); color: var(--td-text-color-brand); font-size: var(--app-text-sm); }
.muted { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); line-height: 1.8; }
.selection-bar { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 12px; padding: 12px 16px; border: 1px solid color-mix(in srgb, var(--td-brand-color) 18%, transparent); border-radius: var(--app-radius-lg); background: var(--td-brand-color-light); font-size: var(--app-text-md);
  strong { font-weight: 500; }
  > div > .muted { display: block; margin-top: 3px; }
  .row { flex-wrap: wrap; gap: 8px; }
}
.source-table { position: relative; border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-panel); background: var(--app-surface-raised); box-shadow: var(--app-shadow-sm); overflow: hidden; }
.source-table-head, .source-row { display: grid; grid-template-columns: 22px minmax(160px, 1fr) 142px 90px 80px; gap: 18px; align-items: center; padding: 13px 20px; }
.source-table-head { min-height: 46px; background: var(--app-surface-muted); color: var(--td-text-color-secondary); font-size: var(--app-text-sm); font-weight: 500; letter-spacing: .015em; }
.action-heading { text-align: right; padding-right: 8px; }
.source-row { min-height: 82px; border-top: 1px solid var(--app-border-subtle); transition: background-color var(--app-motion-fast);
  &:hover { background: var(--td-bg-color-container-hover); }
  &.selected { background: color-mix(in srgb, var(--td-brand-color) 6%, var(--app-surface-raised)); box-shadow: inset 3px 0 0 var(--td-brand-color); }
}
.source-main { display: flex; align-items: center; gap: 13px; min-width: 0; padding: 3px 0; border: 0; border-radius: var(--app-radius-xs); background: none; text-align: left; color: inherit; cursor: pointer; &:hover .source-copy strong { color: var(--td-text-color-link); text-decoration: underline; text-underline-offset: 4px; text-decoration-thickness: 1px; } }
.source-icon { width: 40px; height: 44px; display: grid; place-items: center; flex: none; color: var(--td-text-color-secondary); background: var(--app-surface-muted); border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-md);
  &.url, &.manual { color: var(--td-text-color-brand); background: color-mix(in srgb, var(--td-brand-color) 5%, var(--app-surface-raised)); }
}
.source-copy { min-width: 0; strong, small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  strong { font-size: var(--app-text-base); font-weight: 500; line-height: 1.6; }
  small { margin-top: 5px; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); line-height: 1.5; }
}
.source-status { min-width: 0; }
.state-pill { display: inline-flex; align-items: center; gap: 6px; max-width: 100%; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); white-space: nowrap;
  i { width: 5px; height: 5px; flex: none; border-radius: 50%; background: currentColor; }
  &.success { color: var(--td-success-color-7); }
  &.danger { color: var(--td-error-color); }
  &.processing { color: var(--td-text-color-brand); }
}
.source-error { margin-top: 5px; color: var(--td-error-color); font-size: var(--app-text-xs); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.source-date { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); white-space: nowrap; font-variant-numeric: tabular-nums; }
.source-actions { display: flex; justify-content: flex-end; }
.list-loading { display: flex; align-items: center; justify-content: center; gap: 8px; padding: 9px; background: var(--td-brand-color-light); color: var(--td-text-color-brand); font-size: var(--app-text-sm); }
.source-empty { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 12px; min-height: 270px; padding: 36px 20px; text-align: center;
  h3 { margin-top: 2px; font-size: var(--app-text-xl); font-weight: 500; }
  p { max-width: 420px; color: var(--td-text-color-secondary); font-size: var(--app-text-md); line-height: 1.8; }
  > .t-button { margin-top: 5px; }
}
.empty-workspace { background: var(--app-surface-raised); border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-xl); }
.list-footer, .list-actions, .pagination { display: flex; align-items: center; gap: 10px; }
.list-footer { justify-content: space-between; padding-top: 18px; }
.pagination { gap: 8px; span { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); margin-right: 8px; white-space: nowrap; } }
.jobs-panel { margin-bottom: 16px; padding: 16px 20px; border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-lg); background: var(--app-surface-raised);
  .panel-title { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 10px; h3 { font-size: var(--app-text-base); font-weight: 500; } }
  details { padding: 12px 0; border-top: 1px solid var(--app-border-subtle); }
  summary { display: flex; justify-content: space-between; gap: 16px; font-size: var(--app-text-md); cursor: pointer; }
}
.source-table + .jobs-panel { margin-top: 16px; }
.job-item { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; padding: 10px 0; font-size: var(--app-text-sm); span { overflow-wrap: anywhere; } small { grid-column: 1 / -1; color: var(--td-error-color); } }
.asset-row { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; font-size: var(--app-text-md); }
.danger { color: var(--td-error-color); }
.bookmark-list { max-height: 45vh; overflow: auto; margin: 14px 0;
  > div { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 6px 12px; padding: 14px 0; border-bottom: 1px solid var(--app-border-subtle); }
  strong { font-size: var(--app-text-md); font-weight: 500; overflow-wrap: anywhere; }
  span, small { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
  small { grid-column: 1 / -1; overflow-wrap: anywhere; }
}
.share-form { display: grid; gap: 18px; label { display: grid; gap: 8px; font-size: var(--app-text-base); } > .t-button { justify-self: start; } }
.audience { display: grid; gap: 14px; padding: 16px; border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-lg); > strong { font-size: var(--app-text-md); font-weight: 500; } }
.audience-member { display: flex; align-items: center; gap: 10px; > span:nth-child(2) { flex: 1; min-width: 0; font-size: var(--app-text-md); overflow-wrap: anywhere; small { display: block; margin-top: 3px; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); } } }
.member-avatar { width: 32px; height: 32px; flex: none; display: grid; place-items: center; background: var(--td-brand-color-light); color: var(--td-text-color-brand); border-radius: 50%; }
.source-history { display: grid; gap: 18px; }
.replace-original { padding: 18px; border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-lg); background: var(--app-surface-muted);
  strong { font-size: var(--app-text-base); font-weight: 500; }
  p { margin-top: 7px; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); line-height: 1.8; }
  .row { flex-wrap: wrap; margin-top: 12px; }
  .replacement-name { max-width: 100%; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
}
.version-card { padding: 18px; border: 1px solid var(--app-border-subtle); border-radius: var(--app-radius-lg);
  .version-heading { display: flex; justify-content: space-between; align-items: center; gap: 12px; strong { font-size: var(--app-text-md); font-weight: 500; } span { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); } }
  p { margin: 9px 0; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); overflow-wrap: anywhere; }
  .version-filename { color: var(--td-text-color-primary); }
  .row { flex-wrap: wrap; margin-top: 14px; }
}
.history-actions { flex-wrap: wrap; }
@media (max-width: 1100px) {
  .workspace-header { padding: 26px 24px 22px; }
  .workspace-nav { padding: 10px 24px; }
  .workspace-body { padding: 24px; }
  .dataset-permission { display: none; }
  .source-table-head, .source-row { grid-template-columns: 20px minmax(130px, 1fr) 126px 72px; gap: 14px; padding-left: 16px; padding-right: 16px; }
  .source-date, .date-heading { display: none; }
  .file-drop { flex-wrap: wrap; .row { margin-left: 42px; } }
  .selection-bar { flex-wrap: wrap; }
}
@media (max-width: 720px) {
  .workspace-header { align-items: flex-start; flex-wrap: wrap; gap: 16px; padding: 24px 18px 18px; }
  .identity { h1 { font-size: var(--app-text-3xl); } p { font-size: var(--app-text-sm); } }
  .identity-mark { width: 40px; height: 40px; }
  .header-actions { margin-left: auto; gap: 4px; }
  .workspace-nav { padding: 8px 16px; gap: 4px; overflow-x: auto; button { flex: none; padding: 8px 12px; font-size: var(--app-text-md); white-space: nowrap; } }
  .workspace-body { padding: 20px 16px 28px; }
  .dataset-bar { flex-wrap: wrap; gap: 10px; }
  .dataset-title { width: 100%; }
  .dataset-select { flex: 1; width: auto; min-width: 0; }
  .dataset-bar > .row { margin-left: auto; }
  .content-nav { gap: 12px; flex-wrap: wrap; margin-bottom: 20px; }
  .content-tabs { gap: 24px; }
  .content-links { margin-left: auto; }
  .capture-card { padding: 14px; }
  .capture-heading { flex-wrap: wrap; gap: 10px; margin-bottom: 16px; }
  .capture-modes { flex: 1; button { flex: 1; padding: 7px 8px; white-space: nowrap; } }
  .capture-heading > .t-button { margin-left: auto; }
  .capture-main { flex-wrap: wrap; > .t-input__wrap { flex-basis: 100%; } > .t-button { margin-left: auto; } }
  .capture-foot { flex-wrap: wrap; gap: 10px; > .t-input__wrap { max-width: none; width: 100%; } > span { max-width: 100%; } }
  .file-drop { padding: 16px 12px; gap: 12px; .row { margin-left: 0; width: 100%; flex-wrap: wrap; } }
  .sources-toolbar { align-items: flex-start; flex-direction: column; gap: 14px; margin-top: 24px; }
  .source-controls { width: 100%; gap: 8px; > .t-button { flex: none; } }
  .source-search { flex: 1; width: auto; }
  .source-table-head { grid-template-columns: 20px minmax(0, 1fr); gap: 12px; padding: 12px 14px; }
  .status-heading, .action-heading { display: none; }
  .source-row { grid-template-columns: 20px minmax(0, 1fr) auto; gap: 8px 12px; padding: 15px 14px; }
  .source-checkbox { grid-column: 1; grid-row: 1; align-self: center; }
  .source-main { grid-column: 2 / -1; grid-row: 1; gap: 10px; }
  .source-icon { width: 34px; height: 40px; }
  .source-copy strong { font-size: var(--app-text-md); }
  .source-status { grid-column: 2; grid-row: 2; padding-left: 44px; }
  .source-actions { grid-column: 3; grid-row: 2; }
  .source-error { max-width: 160px; }
  .selection-bar { padding: 12px; gap: 10px; .row { gap: 6px; } }
  .source-empty { min-height: 240px; padding: 30px 20px; }
  .list-footer { flex-wrap: wrap; gap: 16px; }
  .pagination { margin-left: auto; }
  .project-grid { grid-template-columns: 1fr; }
  .empty-project, .setup-card { flex-wrap: wrap; }
  .setup-card { padding: 24px; > div:not(.t-loading) { flex: 1 1 180px; } }
  .section-heading { align-items: flex-start; gap: 12px; }
  .jobs-panel { padding: 14px; summary { flex-wrap: wrap; gap: 8px; } }
}
@media (hover: hover) {
  .project-grid > button:hover { border-color: color-mix(in srgb, var(--td-text-color-link) 35%, var(--app-border-subtle)); box-shadow: var(--app-shadow-md); }
  .content-tabs button:not(.active):hover { color: var(--td-text-color-primary); border-bottom-color: var(--td-component-border); }
}
@media (pointer: coarse) {
  .research-shell :deep(.t-button), .capture-modes button, .workspace-nav button, .search-button { min-height: 44px; }
  .search-button { min-width: 36px; }
}
@media (prefers-reduced-motion: reduce) {
  .workspace-nav button, .project-grid > button, .capture-card, .source-row { transition: none; }
}
</style>
