import type { KnowledgeProcessOverrides } from '@/types/knowledgeProcess'

export type ImageCaptureMode = 'recognize' | 'archive'
export interface CaptureFile { name: string; type?: string }

export function isCaptureImage(file: CaptureFile): boolean {
  return !!file.type?.startsWith('image/') || /\.(png|jpe?g|gif|bmp|webp)$/i.test(file.name)
}

export function imageRecognitionConfigured(kb?: { vlm_config?: { enabled?: boolean; model_id?: string } }): boolean {
  return !!(kb?.vlm_config?.enabled && kb.vlm_config.model_id)
}

/** Archive consent applies only to images in this batch; documents retain normal parsing. */
export function imageCaptureOverrides(file: CaptureFile, mode: ImageCaptureMode, canRecognize: boolean): KnowledgeProcessOverrides | undefined {
  if (!isCaptureImage(file)) return undefined
  if (mode === 'archive') return { image_archive_only: true }
  if (!canRecognize) throw new Error('当前资料集未启用视觉模型。请配置视觉模型，或主动选择“仅保存原图”后上传；原图归档不会识别图片内容。')
  return { image_archive_only: false }
}
