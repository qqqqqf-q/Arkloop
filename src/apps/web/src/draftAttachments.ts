import type { DraftAttachmentRecord } from './storage'
import type { Attachment } from './components/ChatInput'

export function restoreAttachmentFromDraftRecord(record: DraftAttachmentRecord): Attachment {
  return {
    id: record.id,
    name: record.name,
    size: record.size,
    mime_type: record.mime_type,
    status: 'ready',
    uploaded: record.uploaded,
    pasted: record.pasted,
  }
}

export function buildDraftAttachmentRecords(attachments: Attachment[]): DraftAttachmentRecord[] {
  return attachments
    .filter((attachment): attachment is Attachment & { uploaded: NonNullable<Attachment['uploaded']> } => (
      attachment.status === 'ready' && attachment.uploaded != null
    ))
    .map((attachment) => ({
      id: attachment.id,
      name: attachment.name,
      size: attachment.size,
      mime_type: attachment.mime_type,
      status: 'ready' as const,
      uploaded: attachment.uploaded,
      pasted: attachment.pasted,
    }))
}
