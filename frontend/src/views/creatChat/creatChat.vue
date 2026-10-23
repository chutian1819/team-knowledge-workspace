<template>
    <div class="dialogue-wrap">
        <div class="dialogue-answers">
            <header class="dialogue-header" style="--wails-draggable: drag">
                <div class="dialogue-mark" aria-hidden="true"><t-icon name="chat" size="24px" /></div>
                <h1 class="dialogue-title">{{ $t('createChat.title') }}</h1>
            </header>
            <div class="create-chat-composer">
                <div v-if="hostSandboxEnabled" class="project-dir-bar">
                    <button type="button" class="project-dir-bar__btn"
                        :class="{ 'is-bound': !!selectedProjectDir, 'is-picking': pickingProjectDir }"
                        :disabled="pickingProjectDir || creatingSession" :title="selectedProjectDir || $t('createChat.openProject')"
                        @click="openProjectDir">
                        <t-icon :name="pickingProjectDir ? 'loading' : 'folder'" />
                        <span class="project-dir-bar__name">{{
                            selectedProjectDir ? projectDirBasename(selectedProjectDir) : $t('createChat.openProject')
                        }}</span>
                    </button>
                    <button v-if="selectedProjectDir" type="button" class="project-dir-bar__clear"
                        :disabled="creatingSession" :aria-label="$t('createChat.clearProject')" @click="clearProjectDir">×</button>
                </div>
                <InputField ref="inputFieldRef" :composer-locked="creatingSession" preserve-draft-on-send @send-msg="sendMsg"></InputField>
            </div>
            <!-- 推荐问题 -->
            <div ref="sqContainerRef" class="suggested-questions-container">
                <!-- 骨架屏占位 -->
                <div v-if="sqInitialLoad && suggestedQuestions.length === 0" class="suggested-questions-inner">
                    <div class="suggested-questions-title"><t-skeleton animation="gradient"
                            :row-col="[{ width: '120px', height: '14px' }]" /></div>
                    <div class="suggested-questions-grid">
                        <div v-for="n in 6" :key="'sq-skel-' + n" class="suggested-question-card sq-card-skeleton">
                            <t-skeleton animation="gradient"
                                :row-col="[{ width: '100%', height: '14px', type: 'rect' }]" />
                        </div>
                    </div>
                </div>
                <transition v-else appear name="sq-slide-fade" mode="out-in" @before-leave="onBeforeLeave"
                    @after-leave="onAfterLeave" @enter="onEnter" @after-enter="onQuestionsEntered">
                    <div v-if="suggestedQuestions.length > 0" :key="sqRenderKey" class="suggested-questions-inner">
                        <div class="suggested-questions-title-row">
                            <p class="suggested-questions-caption">
                                <span class="suggested-questions-title">{{ $t('chat.suggestedQuestions') }}</span>
                                <button type="button" class="suggested-questions-refresh" :disabled="sqLoading"
                                    :title="$t('chat.refreshSuggestedQuestions')"
                                    :aria-label="$t('chat.refreshSuggestedQuestions')" @click="fetchSuggestedQuestions(true)">
                                    <t-icon :name="sqLoading ? 'loading' : 'refresh'"
                                        :class="{ 'sq-refresh-spin': sqLoading }" />
                                </button>
                            </p>
                        </div>
                        <div class="suggested-questions-grid">
                            <button v-for="item in suggestedQuestions" :key="item.question" type="button"
                                class="suggested-question-card" :disabled="creatingSession"
                                @click="handleSuggestedQuestionClick(item)">
                                <span class="suggested-question-text">{{ item.question }}</span>
                                <span v-if="item.source === 'faq'" class="suggested-question-badge faq">FAQ</span>
                            </button>
                        </div>
                    </div>
                </transition>
            </div>

        </div>
    </div>

    <ContextualGuide tour="chat" :when="showChatContextualGuide" />

    <!-- 知识库编辑器（创建/编辑统一组件） -->
    <KnowledgeBaseEditorModal :visible="uiStore.showKBEditorModal" :mode="uiStore.kbEditorMode"
        :kb-id="uiStore.currentKBId || undefined" :initial-type="uiStore.kbEditorType"
        @update:visible="(val) => val ? null : uiStore.closeKBEditor()" @success="handleKBEditorSuccess" />
</template>
<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount, computed } from 'vue';
import ContextualGuide from '@/components/ContextualGuide.vue';
import InputField from '@/components/Input-field.vue';
import { createSessions } from "@/api/chat/index";
import { pickHostProjectDir } from '@/utils/desktopProjectDir';
import { projectDirBasename, shouldRenderHostProjectSettings, withOptionalProjectDir } from '@/utils/hostWorkspace';
import { getSuggestedQuestions } from "@/api/agent/index";
import type { SuggestedQuestion } from "@/api/agent/index";
import { questionOriginFromSuggestion, type SendMessageOptions } from '@/utils/questionOrigin';
import { useMenuStore } from '@/stores/menu';
import { useSettingsStore } from '@/stores/settings';
import { useUIStore } from '@/stores/ui';
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities';
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router';
import { MessagePlugin } from 'tdesign-vue-next';
import { useI18n } from 'vue-i18n';
import KnowledgeBaseEditorModal from '@/views/knowledge/KnowledgeBaseEditorModal.vue';
import { useKnowledgeBaseCreationNavigation } from '@/hooks/useKnowledgeBaseCreationNavigation';

const router = useRouter();
const route = useRoute();
const usemenuStore = useMenuStore();
const settingsStore = useSettingsStore();
onBeforeRouteLeave((to) => {
    // The first send carries the draft into its new session; abandoning the
    // composer must not make this a default for the next conversation.
    if (!to.path.startsWith('/platform/chat/') || !usemenuStore.isFirstSession) {
        settingsStore.reasoningEffortOverride = '';
    }
});
const uiStore = useUIStore();
const deploymentCapabilities = useDeploymentCapabilitiesStore();
const { t } = useI18n();
const { navigateToKnowledgeBaseList } = useKnowledgeBaseCreationNavigation();

const hostSandboxEnabled = computed(() =>
    shouldRenderHostProjectSettings(deploymentCapabilities.isSupported('settings.sandbox.host')),
);
const selectedProjectDir = ref('');
const pickingProjectDir = ref(false);
const creatingSession = ref(false);
let pageDisposed = false;

const showChatContextualGuide = computed(() => {
    return route.name === 'globalCreatChat' || route.name === 'kbCreatChat';
});

// ===== 推荐问题 =====
const suggestedQuestions = ref<SuggestedQuestion[]>([]);
const sqLoading = ref(true);
// The skeleton reserves space only until the first answer arrives. Later
// refetches keep the current content in place, so the composer below does not
// jump each time the agent or @mention selection changes.
const sqInitialLoad = ref(true);
const sqRenderKey = ref(0);
const sqContainerRef = ref<HTMLElement | null>(null);
let suggestedQuestionsFetchId = 0;
let lastSuggestedQuestionsKey = '';
let debounceTimer: ReturnType<typeof setTimeout> | null = null;

const sameSuggestedQuestions = (a: SuggestedQuestion[], b: SuggestedQuestion[]) =>
    a.length === b.length && a.every((item, i) => item.question === b[i].question && item.source === b[i].source);

// --- 高度平滑过渡钩子 ---
const onBeforeLeave = () => {
    const c = sqContainerRef.value;
    if (!c) return;
    c.style.height = c.offsetHeight + 'px';
    c.style.overflow = 'hidden';
};

const onAfterLeave = () => {
    const c = sqContainerRef.value;
    if (!c) return;
    if (suggestedQuestions.value.length === 0) {
        requestAnimationFrame(() => { c.style.height = '0px'; });
        c.addEventListener('transitionend', () => {
            c.style.height = '';
            c.style.overflow = '';
        }, { once: true });
    }
};

const onEnter = (el: Element) => {
    const c = sqContainerRef.value;
    if (!c) return;
    const startHeight = c.offsetHeight;
    c.style.height = 'auto';
    c.style.overflow = 'hidden';
    const targetHeight = c.offsetHeight;
    c.style.height = startHeight + 'px';
    requestAnimationFrame(() => {
        c.style.height = targetHeight + 'px';
    });
};

const onQuestionsEntered = () => {
    const c = sqContainerRef.value;
    if (c) {
        c.style.height = '';
        c.style.overflow = '';
    }
};

const fetchSuggestedQuestions = async (force = false) => {
    const agentId = settingsStore.selectedAgentId;
    const params = settingsStore.getSuggestedQuestionsParams();
    const requestKey = JSON.stringify([agentId, params]);
    if (!force && requestKey === lastSuggestedQuestionsKey) return;
    lastSuggestedQuestionsKey = requestKey;

    const fetchId = ++suggestedQuestionsFetchId;
    sqLoading.value = true;
    try {
        if (!agentId) {
            suggestedQuestions.value = [];
            return;
        }
        const res = await getSuggestedQuestions(agentId, params);
        if (fetchId === suggestedQuestionsFetchId) {
            const next = res?.data?.questions || [];
            if (!sameSuggestedQuestions(suggestedQuestions.value, next)) {
                sqRenderKey.value++;
                suggestedQuestions.value = next;
            }
        }
    } catch (err) {
        console.warn('[SuggestedQuestions] Failed to fetch:', err);
        if (fetchId === suggestedQuestionsFetchId) {
            lastSuggestedQuestionsKey = '';
            suggestedQuestions.value = [];
        }
    } finally {
        if (fetchId === suggestedQuestionsFetchId) {
            sqLoading.value = false;
            sqInitialLoad.value = false;
        }
    }
};

// 防抖包装，切换知识库/文件时300ms内不重复请求
const debouncedFetch = () => {
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => { fetchSuggestedQuestions(); }, 300);
};

// 监听 Agent / 知识库 / 文件 / 标签 @mention；MCP 与 Skill 不影响推荐范围
watch(
    () => ({
        agentId: settingsStore.selectedAgentId,
        kbs: settingsStore.settings.selectedKnowledgeBases,
        files: settingsStore.settings.selectedFiles,
        tags: settingsStore.settings.selectedTags,
    }),
    debouncedFetch,
    { deep: true },
);

onMounted(() => {
    fetchSuggestedQuestions();
});

onBeforeUnmount(() => {
    pageDisposed = true;
    if (debounceTimer) clearTimeout(debounceTimer);
    suggestedQuestionsFetchId++;
});

const inputFieldRef = ref();

// The suggestion's source rides with this send to the new session's first
// request, so the agent searches it before answering.
const handleSuggestedQuestionClick = (item: SuggestedQuestion) => {
    inputFieldRef.value?.triggerSend(item.question, { questionOrigin: questionOriginFromSuggestion(item) });
};

const sendMsg = (value: string, modelId: string, mentionedItems: any[], imageFiles: any[] = [], attachmentFiles: any[] = [], options: SendMessageOptions = {}) => {
    createNewSession(value, modelId, mentionedItems, imageFiles, attachmentFiles, options);
}

async function createNewSession(value: string, modelId: string, mentionedItems: any[] = [], imageFiles: any[] = [], attachmentFiles: any[] = [], options: SendMessageOptions = {}) {
    if (creatingSession.value || pageDisposed || !value.trim()) return;
    creatingSession.value = true;
    const selectedKbs = settingsStore.settings.selectedKnowledgeBases || [];
    const selectedFiles = settingsStore.settings.selectedFiles || [];

    // 构建 session 数据，包含 Agent 配置
    const sessionData: any = {};

    // 添加 Agent 配置（知识库信息在 agent_config 中）
    sessionData.agent_config = {
        enabled: true,
        max_iterations: settingsStore.agentConfig.maxIterations,
        temperature: settingsStore.agentConfig.temperature,
        knowledge_bases: selectedKbs,  // 所有选中的知识库
        knowledge_ids: selectedFiles,  // 所有选中的普通知识/文件
        allowed_tools: settingsStore.agentConfig.allowedTools
    };

    try {
        const res = await createSessions(withOptionalProjectDir(sessionData, selectedProjectDir.value));
        if (pageDisposed) return;
        if (res.data && res.data.id) {
            await navigateToSession(res.data.id, value, modelId, mentionedItems, imageFiles, attachmentFiles, options);
        } else {
            console.error('[createChat] Failed to create session');
            MessagePlugin.error(t('createChat.messages.createFailed'));
        }
    } catch (error) {
        console.error('[createChat] Create session error:', error);
        if (!pageDisposed) MessagePlugin.error(t('createChat.messages.createError'));
    } finally {
        creatingSession.value = false;
    }
}

const navigateToSession = async (sessionId: string, value: string, modelId: string, mentionedItems: any[], imageFiles: any[] = [], attachmentFiles: any[] = [], options: SendMessageOptions = {}) => {
    const now = new Date().toISOString();
    let obj = {
        title: t('createChat.newSessionTitle'),
        path: `chat/${sessionId}`,
        id: sessionId,
        isMore: false,
        isNoTitle: true,
        created_at: now,
        updated_at: now
    };
    usemenuStore.updataMenuChildren(obj);
    usemenuStore.changeIsFirstSession(true);
    usemenuStore.changeFirstQuery(value, mentionedItems, modelId, imageFiles, attachmentFiles, options.questionOrigin ?? null);
    await router.push(`/platform/chat/${sessionId}`);
}

const handleKBEditorSuccess = (kbId: string) => {
    navigateToKnowledgeBaseList(kbId)
}

function clearProjectDir() {
    selectedProjectDir.value = '';
}

async function openProjectDir() {
    if (pickingProjectDir.value || creatingSession.value) return;
    pickingProjectDir.value = true;
    try {
        const picked = await pickHostProjectDir();
        if (picked) selectedProjectDir.value = picked;
    } catch (e: any) {
        MessagePlugin.error(e?.message || t('createChat.pickFailed'));
    } finally {
        pickingProjectDir.value = false;
    }
}

</script>
<style lang="less" scoped>
@import '../../components/css/suggested-questions.less';

.dialogue-wrap {
    flex: 1;
    display: flex;
    justify-content: center;
    align-items: flex-start;
    min-width: 0;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior-y: contain;
    padding: 56px 40px 88px;
    box-sizing: border-box;
}
.dialogue-answers {
    margin-block: auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: stretch;
    width: 100%;
    max-width: 800px;
    gap: 28px;
    :deep(.answers-input) {
        position: static;
        transform: none;
        z-index: auto;
    }
    :deep(.rich-input-container) {
        background: var(--app-surface-raised);
        border-color: var(--app-border-subtle);
        box-shadow: var(--app-shadow-md);
    }
    :deep(.rich-input-container:focus-within) {
        border-color: var(--td-brand-color);
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--td-brand-color) 7%, transparent), var(--app-shadow-md);
    }
}
.dialogue-header { display: flex; flex-direction: column; align-items: flex-start; gap: 18px; }
.dialogue-mark {
    display: grid;
    place-items: center;
    width: 48px;
    height: 48px;
    border: 1px solid var(--app-border-subtle);
    border-radius: var(--app-radius-xl);
    background: var(--app-surface-raised);
    color: var(--td-text-color-brand);
    box-shadow: var(--app-shadow-sm);
}
.dialogue-title {
    margin: 0;
    color: var(--td-text-color-primary);
    font-size: var(--app-text-4xl);
    font-weight: 600;
    line-height: 1.4;
    letter-spacing: -0.025em;
}
.create-chat-composer { display: flex; flex-direction: column; align-items: stretch; gap: 10px; width: 100%; }
.project-dir-bar { display: flex; align-items: center; gap: 6px; width: 100%; box-sizing: border-box; }
.project-dir-bar__btn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    max-width: 100%;
    min-height: 32px;
    padding: 5px 10px;
    border: 1px solid var(--app-border-subtle);
    border-radius: var(--app-radius-md);
    background: var(--app-surface-raised);
    color: var(--td-text-color-secondary);
    font-size: var(--app-text-sm);
    cursor: pointer;
    &.is-bound { color: var(--td-text-color-primary); }
    &:hover:not(:disabled) { border-color: var(--td-brand-color); color: var(--td-text-color-brand); }
    &:disabled { cursor: default; opacity: 0.65; }
}
.project-dir-bar__name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.project-dir-bar__clear {
    flex: 0 0 auto;
    width: 32px;
    height: 32px;
    border: none;
    border-radius: var(--app-radius-md);
    background: transparent;
    color: var(--td-text-color-secondary);
    font-size: var(--app-text-2xl);
    cursor: pointer;
    &:hover { background: var(--app-surface-muted); }
}
.suggested-questions-container { max-width: 100%; margin: 0; padding: 0; box-sizing: border-box; transition: height var(--app-motion-base) @suggested-ease; }
.suggested-questions-title-row { margin-bottom: 12px; text-align: left; }
.suggested-questions-title { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
.suggested-questions-refresh { width: 28px; height: 28px; color: var(--td-text-color-secondary); }
.suggested-questions-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.suggested-question-card {
    justify-content: space-between;
    width: 100%;
    min-height: 50px;
    padding: 13px 16px;
    border-radius: var(--app-radius-md);
    border-color: var(--app-border-subtle);
    background: var(--app-surface-raised);
    box-shadow: none;
    text-align: left;
    font: inherit;
    transition: background var(--app-motion-fast), border-color var(--app-motion-fast);
    &.sq-card-skeleton, &.sq-card-skeleton:nth-child(n) { width: 100%; }
    &:disabled { opacity: 0.6; cursor: default; }
    &:not(.sq-card-skeleton):not(:disabled):hover { border-color: var(--td-brand-color); background: var(--app-surface-muted); box-shadow: none; }
}
.suggested-question-text { white-space: normal; overflow-wrap: anywhere; }
.sq-slide-fade-enter-active, .sq-slide-fade-leave-active { transition: opacity var(--app-motion-fast); }
.sq-slide-fade-enter-from, .sq-slide-fade-leave-to { opacity: 0; }
button:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }

@media (max-width: 720px) {
    .dialogue-wrap { padding: 28px 16px max(28px, env(safe-area-inset-bottom)); }
    .dialogue-answers { gap: 24px; margin-block: 0; }
    .dialogue-header { gap: 14px; }
    .suggested-questions-grid { grid-template-columns: minmax(0, 1fr); }
}
@media (prefers-reduced-motion: reduce) {
    .suggested-questions-container, .suggested-question-card, .sq-slide-fade-enter-active, .sq-slide-fade-leave-active { transition: none; }
    .sq-refresh-spin { animation: none; }
}
</style>
