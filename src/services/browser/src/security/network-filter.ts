import { isIP } from 'node:net';
import { resolve4, resolve6 } from 'node:dns/promises';

export type BlockedReason =
  | 'private_ip'
  | 'loopback'
  | 'link_local'
  | 'cloud_metadata'
  | 'internal_service';

export interface NetworkFilterResult {
  blocked: boolean;
  reason?: BlockedReason;
  target?: string;
}

export const CLOUD_METADATA_HOSTS = [
  'metadata.google.internal',
  '169.254.169.254',
] as const;

interface CidrRule {
  ip: number;
  mask: number;
  reason: BlockedReason;
}

// 预计算 CIDR 规则，避免每次调用都解析
const CIDR_RULES: CidrRule[] = (
  [
    { cidr: '10.0.0.0/8', reason: 'private_ip' as const },
    { cidr: '172.16.0.0/12', reason: 'private_ip' as const },
    { cidr: '192.168.0.0/16', reason: 'private_ip' as const },
    { cidr: '169.254.0.0/16', reason: 'link_local' as const },
    { cidr: '127.0.0.0/8', reason: 'loopback' as const },
  ] as const
).map(({ cidr, reason }) => {
  const [ipStr, bits] = cidr.split('/');
  return { ip: ipToInt(ipStr), mask: ~((1 << (32 - parseInt(bits, 10))) - 1), reason };
});

function ipToInt(ip: string): number {
  const parts = ip.split('.');
  return ((parseInt(parts[0], 10) << 24) | (parseInt(parts[1], 10) << 16) | (parseInt(parts[2], 10) << 8) | parseInt(parts[3], 10)) >>> 0;
}

// 从 IPv4-mapped IPv6 地址中提取 IPv4 部分
function extractIPv4FromMapped(addr: string): string | null {
  const lower = addr.toLowerCase();
  // ::ffff:1.2.3.4
  if (lower.startsWith('::ffff:')) {
    const v4Part = lower.slice(7);
    if (isIP(v4Part) === 4) return v4Part;
  }
  return null;
}

function checkIPv4(ip: string): NetworkFilterResult {
  const ipInt = ipToInt(ip);
  for (const rule of CIDR_RULES) {
    if ((ipInt & rule.mask) === (rule.ip & rule.mask)) {
      return { blocked: true, reason: rule.reason, target: ip };
    }
  }
  return { blocked: false };
}

function checkIPv6(ip: string): NetworkFilterResult {
  // IPv4-mapped IPv6 => 提取 v4 部分检查
  const v4 = extractIPv4FromMapped(ip);
  if (v4) return checkIPv4(v4);

  // ::1 loopback
  const normalized = ip.toLowerCase().replace(/\b0+/g, '');
  if (normalized === '::1' || normalized === '0:0:0:0:0:0:0:1') {
    return { blocked: true, reason: 'loopback', target: ip };
  }

  return { blocked: false };
}

// 同步检查：仅基于 hostname 字面值判断（IP 段匹配 + 已知主机名）
export function isBlockedTarget(hostname: string, blockedHosts: string[]): NetworkFilterResult {
  const lower = hostname.toLowerCase();

  // 云元数据服务
  if ((CLOUD_METADATA_HOSTS as readonly string[]).includes(lower)) {
    return { blocked: true, reason: 'cloud_metadata', target: hostname };
  }

  // compose 内部服务名
  if (blockedHosts.some((h) => lower === h.toLowerCase())) {
    return { blocked: true, reason: 'internal_service', target: hostname };
  }

  // localhost
  if (lower === 'localhost') {
    return { blocked: true, reason: 'loopback', target: hostname };
  }

  const ipVersion = isIP(hostname);
  if (ipVersion === 4) return checkIPv4(hostname);
  if (ipVersion === 6) return checkIPv6(hostname);

  return { blocked: false };
}

export interface DnsResolver {
  resolve4(hostname: string): Promise<string[]>;
  resolve6(hostname: string): Promise<string[]>;
}

const defaultResolver: DnsResolver = { resolve4, resolve6 };

// 异步检查：DNS 预解析后检查解析结果是否落入拦截范围
export async function checkNetworkAccess(
  hostname: string,
  blockedHosts: string[],
  resolver: DnsResolver = defaultResolver,
): Promise<NetworkFilterResult> {
  // 先做同步检查
  const syncResult = isBlockedTarget(hostname, blockedHosts);
  if (syncResult.blocked) return syncResult;

  // 非 IP 地址 hostname 需要 DNS 预解析
  if (isIP(hostname) !== 0) return { blocked: false };

  const resolved: string[] = [];
  try {
    const v4 = await resolver.resolve4(hostname);
    resolved.push(...v4);
  } catch {
    // DNS 解析失败不阻塞，交给 Playwright 处理
  }
  try {
    const v6 = await resolver.resolve6(hostname);
    resolved.push(...v6);
  } catch {
    // 同上
  }

  for (const ip of resolved) {
    const ipVersion = isIP(ip);
    let result: NetworkFilterResult;
    if (ipVersion === 4) {
      result = checkIPv4(ip);
    } else if (ipVersion === 6) {
      result = checkIPv6(ip);
    } else {
      continue;
    }
    if (result.blocked) {
      return { blocked: true, reason: result.reason, target: `${hostname} -> ${ip}` };
    }
  }

  return { blocked: false };
}
