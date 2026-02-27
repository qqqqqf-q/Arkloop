import type { Page, BrowserContext } from 'playwright';
import type { StorageClient } from '../storage/minio-client.js';
import type { Config } from '../config.js';

export interface PageState {
  pageUrl: string;
  pageTitle: string;
  screenshotUrl: string;
  contentText: string;
  accessibilityTree: string;
}

// 从 context 获取活跃 page，没有则创建新 page。
export async function getActivePage(context: BrowserContext): Promise<Page> {
  const pages = context.pages();
  if (pages.length > 0) return pages[0];
  return context.newPage();
}

// 截图 + 提取页面文本 + 无障碍树，返回统一的页面状态。
export async function capturePageState(
  page: Page,
  storage: StorageClient,
  runId: string,
  contentTextMaxLength: number,
): Promise<PageState> {
  const [screenshotBuffer, pageUrl, pageTitle, rawText, a11ySnapshot] = await Promise.all([
    page.screenshot({ type: 'png' }),
    page.url(),
    page.title(),
    page.innerText('body').catch(() => ''),
    page.accessibility.snapshot().catch(() => null),
  ]);

  const step = Date.now();
  const screenshotUrl = await storage.uploadScreenshot(runId, step, Buffer.from(screenshotBuffer));
  const contentText = truncateText(rawText, contentTextMaxLength);
  const accessibilityTree = a11ySnapshot ? formatAccessibilityTree(a11ySnapshot) : '';

  return { pageUrl, pageTitle, screenshotUrl, contentText, accessibilityTree };
}

interface AXNode {
  role: string;
  name?: string;
  value?: string | number;
  description?: string;
  children?: AXNode[];
}

// 将 Playwright accessibility snapshot 格式化为紧凑文本。
export function formatAccessibilityTree(root: AXNode, maxDepth = 6): string {
  const lines: string[] = [];

  function walk(node: AXNode, depth: number): void {
    if (depth > maxDepth) return;
    // 跳过无意义的容器节点（无 name 且无 value 的 generic/none）
    const skip = (node.role === 'none' || node.role === 'generic') && !node.name && !node.value;
    const nextDepth = skip ? depth : depth + 1;

    if (!skip) {
      const indent = '  '.repeat(depth);
      let label = `[${node.role}`;
      if (node.name) label += ` "${node.name}"`;
      if (node.value) label += ` value="${node.value}"`;
      label += ']';
      lines.push(`${indent}${label}`);
    }

    if (node.children) {
      for (const child of node.children) {
        walk(child, nextDepth);
      }
    }
  }

  walk(root, 0);
  return lines.join('\n');
}

export function truncateText(text: string, maxLength: number): string {
  if (text.length <= maxLength) return text;
  return text.slice(0, maxLength);
}
