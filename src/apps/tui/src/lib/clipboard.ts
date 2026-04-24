import { randomUUID } from "node:crypto"
import { spawnSync } from "node:child_process"
import { existsSync, readFileSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { decodePasteBytes, type PasteEvent } from "@opentui/core"
import type { PendingImageAttachment } from "../api/types"

const CLIPBOARD_IMAGE_MIMES = ["image/png", "image/jpeg", "image/bmp", "image/gif", "image/webp"] as const
const MAX_CLIPBOARD_BYTES = 16 << 20

export function resolvePastedImage(event: PasteEvent): PendingImageAttachment | null {
  const directImage = readImageFromPasteEvent(event)
  if (directImage) return directImage
  if (decodePasteBytes(event.bytes).length > 0) return null
  return readSystemClipboardImage()
}

function readImageFromPasteEvent(event: PasteEvent): PendingImageAttachment | null {
  if (event.bytes.length === 0) return null
  const mimeType = normalizeImageMimeType(event.metadata?.mimeType, event.bytes)
  if (!mimeType) return null
  return {
    filename: createPastedFilename(mimeType),
    mimeType,
    size: event.bytes.byteLength,
    bytes: new Uint8Array(event.bytes),
  }
}

function readSystemClipboardImage(): PendingImageAttachment | null {
  switch (process.platform) {
    case "darwin":
      return readMacClipboardImage()
    case "win32":
      return readWindowsClipboardImage()
    default:
      return readLinuxClipboardImage()
  }
}

function readMacClipboardImage(): PendingImageAttachment | null {
  const path = createTempPath("png")
  const result = spawnSync("osascript", [
    "-e", `set outPath to POSIX file "${escapeAppleScript(path)}"`,
    "-e", "try",
    "-e", "  set imageData to the clipboard as «class PNGf»",
    "-e", "on error",
    "-e", "  return",
    "-e", "end try",
    "-e", "set fileRef to open for access outPath with write permission",
    "-e", "set eof fileRef to 0",
    "-e", "write imageData to fileRef",
    "-e", "close access fileRef",
  ], { maxBuffer: MAX_CLIPBOARD_BYTES })
  if (result.status !== 0 || !existsSync(path)) {
    cleanupTempFile(path)
    return null
  }
  return readTempImage(path, "image/png")
}

function readWindowsClipboardImage(): PendingImageAttachment | null {
  const path = createTempPath("png")
  const script = [
    "Add-Type -AssemblyName System.Windows.Forms",
    "Add-Type -AssemblyName System.Drawing",
    "$image = Get-Clipboard -Format Image",
    "if ($null -eq $image) { exit 3 }",
    `$path = '${escapePowerShell(path)}'`,
    "$image.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)",
    "$image.Dispose()",
  ].join("; ")

  for (const command of ["pwsh", "powershell.exe", "powershell"]) {
    const result = spawnSync(command, ["-NoProfile", "-Command", script], { maxBuffer: MAX_CLIPBOARD_BYTES })
    if (result.status === 0 && existsSync(path)) {
      return readTempImage(path, "image/png")
    }
  }

  cleanupTempFile(path)
  return null
}

function readLinuxClipboardImage(): PendingImageAttachment | null {
  for (const mimeType of CLIPBOARD_IMAGE_MIMES) {
    const wayland = spawnSync("wl-paste", ["--no-newline", "--type", mimeType], { maxBuffer: MAX_CLIPBOARD_BYTES })
    const waylandImage = readCommandImage(wayland, mimeType)
    if (waylandImage) return waylandImage
  }

  for (const mimeType of CLIPBOARD_IMAGE_MIMES) {
    const xclip = spawnSync("xclip", ["-selection", "clipboard", "-t", mimeType, "-o"], { maxBuffer: MAX_CLIPBOARD_BYTES })
    const xclipImage = readCommandImage(xclip, mimeType)
    if (xclipImage) return xclipImage
  }

  return null
}

function readCommandImage(
  result: ReturnType<typeof spawnSync>,
  fallbackMimeType: string,
): PendingImageAttachment | null {
  if (result.status !== 0 || !result.stdout || result.stdout.length === 0) return null
  const stdout = typeof result.stdout === "string" ? Buffer.from(result.stdout) : result.stdout
  const bytes = new Uint8Array(stdout)
  const mimeType = normalizeImageMimeType(fallbackMimeType, bytes)
  if (!mimeType) return null
  return {
    filename: createPastedFilename(mimeType),
    mimeType,
    size: bytes.byteLength,
    bytes,
  }
}

function readTempImage(path: string, fallbackMimeType: string): PendingImageAttachment | null {
  try {
    const bytes = new Uint8Array(readFileSync(path))
    const mimeType = normalizeImageMimeType(fallbackMimeType, bytes)
    if (!mimeType) return null
    return {
      filename: createPastedFilename(mimeType),
      mimeType,
      size: bytes.byteLength,
      bytes,
    }
  } finally {
    cleanupTempFile(path)
  }
}

function normalizeImageMimeType(rawMimeType: string | undefined, bytes: Uint8Array): string | null {
  const mimeType = rawMimeType?.trim().toLowerCase()
  if (mimeType?.startsWith("image/")) return mimeType
  return sniffImageMimeType(bytes)
}

function sniffImageMimeType(bytes: Uint8Array): string | null {
  if (bytes.length >= 8 &&
    bytes[0] === 0x89 &&
    bytes[1] === 0x50 &&
    bytes[2] === 0x4e &&
    bytes[3] === 0x47 &&
    bytes[4] === 0x0d &&
    bytes[5] === 0x0a &&
    bytes[6] === 0x1a &&
    bytes[7] === 0x0a) {
    return "image/png"
  }

  if (bytes.length >= 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) {
    return "image/jpeg"
  }

  if (bytes.length >= 6) {
    const header = String.fromCharCode(...bytes.slice(0, 6))
    if (header === "GIF87a" || header === "GIF89a") {
      return "image/gif"
    }
  }

  if (bytes.length >= 12 &&
    String.fromCharCode(...bytes.slice(0, 4)) === "RIFF" &&
    String.fromCharCode(...bytes.slice(8, 12)) === "WEBP") {
    return "image/webp"
  }

  if (bytes.length >= 2 && bytes[0] === 0x42 && bytes[1] === 0x4d) {
    return "image/bmp"
  }

  return null
}

function createPastedFilename(mimeType: string): string {
  return `pasted-${Date.now()}.${imageExtension(mimeType)}`
}

function imageExtension(mimeType: string): string {
  switch (mimeType) {
    case "image/jpeg":
      return "jpg"
    case "image/gif":
      return "gif"
    case "image/webp":
      return "webp"
    case "image/bmp":
      return "bmp"
    default:
      return "png"
  }
}

function createTempPath(ext: string): string {
  return join(tmpdir(), `${randomUUID()}.${ext}`)
}

function cleanupTempFile(path: string) {
  rmSync(path, { force: true })
}

function escapeAppleScript(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, "\\\"")
}

function escapePowerShell(value: string): string {
  return value.replace(/'/g, "''")
}
