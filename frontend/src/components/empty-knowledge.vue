<script setup lang="ts">
import { MAX_FILE_SIZE_MB } from '@/utils'
</script>
<template>
    <div class="empty">
        <span class="empty-img" aria-hidden="true"><t-icon name="folder-add" size="30px" /></span>
        <span class="empty-txt">{{ $t('knowledgeBase.emptyKnowledgeDragDrop') }}</span>
        <span class="empty-type-txt">{{ $t('knowledgeBase.pdfDocFormat', { size: MAX_FILE_SIZE_MB }) }}</span>
        <span class="empty-type-txt">{{ $t('knowledgeBase.textMarkdownFormat', { size: MAX_FILE_SIZE_MB }) }}</span>
    </div>
</template>
<style scoped lang="less">
.empty {
    flex: 0 0 auto;
    display: flex;
    flex-flow: column;
    justify-content: center;
    align-items: center;
}

.empty-txt {
    color: var(--td-text-color-primary);
    font-family: var(--app-font-family);
    font-size: var(--app-text-xl);
    font-weight: 500;
    line-height: 26px;
    margin: 12px 0 16px 0;
}

.empty-type-txt {
    color: var(--td-text-color-secondary);
    text-align: center;
    font-family: var(--app-font-family);
    font-size: var(--app-text-sm);
    font-weight: 400;
    max-width: 360px;
    line-height: 1.8;
}

.empty-img {
    display: grid;
    place-items: center;
    width: 64px;
    height: 64px;
    border: 1px solid var(--app-border-subtle);
    border-radius: var(--app-radius-xl);
    background: var(--app-surface-muted);
    color: var(--td-text-color-secondary);
}
</style>
