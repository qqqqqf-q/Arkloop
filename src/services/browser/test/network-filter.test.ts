import { describe, it, expect } from 'vitest';
import { isBlockedTarget, checkNetworkAccess, type DnsResolver } from '../src/security/network-filter.js';

const BLOCKED_HOSTS = ['postgres', 'redis', 'minio', 'openviking', 'api', 'worker', 'gateway', 'pgbouncer'];

function mockResolver(v4: string[] | Error, v6: string[] | Error): DnsResolver {
  return {
    resolve4: () => (v4 instanceof Error ? Promise.reject(v4) : Promise.resolve(v4)),
    resolve6: () => (v6 instanceof Error ? Promise.reject(v6) : Promise.resolve(v6)),
  };
}

// --- isBlockedTarget (同步) ---

describe('isBlockedTarget', () => {
  describe('RFC1918 private addresses', () => {
    it.each([
      '10.0.0.1',
      '10.255.255.255',
      '172.16.0.1',
      '172.31.255.255',
      '192.168.0.1',
      '192.168.255.255',
    ])('blocks %s', (ip) => {
      const result = isBlockedTarget(ip, BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      expect(result.reason).toBe('private_ip');
    });

    it('allows 172.15.255.255 (outside 172.16.0.0/12)', () => {
      const result = isBlockedTarget('172.15.255.255', BLOCKED_HOSTS);
      expect(result.blocked).toBe(false);
    });

    it('allows 11.0.0.1 (outside 10.0.0.0/8)', () => {
      const result = isBlockedTarget('11.0.0.1', BLOCKED_HOSTS);
      expect(result.blocked).toBe(false);
    });

    it('allows 172.32.0.1 (outside 172.16.0.0/12)', () => {
      const result = isBlockedTarget('172.32.0.1', BLOCKED_HOSTS);
      expect(result.blocked).toBe(false);
    });
  });

  describe('loopback', () => {
    it.each(['127.0.0.1', '127.255.255.255'])('blocks %s', (ip) => {
      const result = isBlockedTarget(ip, BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      expect(result.reason).toBe('loopback');
    });

    it('blocks localhost', () => {
      const result = isBlockedTarget('localhost', BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      expect(result.reason).toBe('loopback');
    });

    it('blocks ::1', () => {
      const result = isBlockedTarget('::1', BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      expect(result.reason).toBe('loopback');
    });
  });

  describe('link-local', () => {
    it.each(['169.254.0.1', '169.254.169.254'])('blocks %s', (ip) => {
      const result = isBlockedTarget(ip, BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      // 169.254.169.254 同时也是云元数据，但先命中 cloud_metadata
      if (ip === '169.254.169.254') {
        expect(result.reason).toBe('cloud_metadata');
      } else {
        expect(result.reason).toBe('link_local');
      }
    });
  });

  describe('cloud metadata', () => {
    it('blocks metadata.google.internal', () => {
      const result = isBlockedTarget('metadata.google.internal', BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      expect(result.reason).toBe('cloud_metadata');
    });

    it('blocks 169.254.169.254', () => {
      const result = isBlockedTarget('169.254.169.254', BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      expect(result.reason).toBe('cloud_metadata');
    });
  });

  describe('internal service names', () => {
    it.each(BLOCKED_HOSTS)('blocks %s', (host) => {
      const result = isBlockedTarget(host, BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      expect(result.reason).toBe('internal_service');
    });

    it('blocks case-insensitively', () => {
      const result = isBlockedTarget('Redis', BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      expect(result.reason).toBe('internal_service');
    });
  });

  describe('IPv4-mapped IPv6', () => {
    it('blocks ::ffff:10.0.0.1', () => {
      const result = isBlockedTarget('::ffff:10.0.0.1', BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      expect(result.reason).toBe('private_ip');
    });

    it('blocks ::ffff:127.0.0.1', () => {
      const result = isBlockedTarget('::ffff:127.0.0.1', BLOCKED_HOSTS);
      expect(result.blocked).toBe(true);
      expect(result.reason).toBe('loopback');
    });

    it('allows ::ffff:8.8.8.8', () => {
      const result = isBlockedTarget('::ffff:8.8.8.8', BLOCKED_HOSTS);
      expect(result.blocked).toBe(false);
    });
  });

  describe('public addresses (allowed)', () => {
    it.each([
      '8.8.8.8',
      '1.1.1.1',
      '142.250.80.46',
      'example.com',
      'www.google.com',
    ])('allows %s', (host) => {
      const result = isBlockedTarget(host, BLOCKED_HOSTS);
      expect(result.blocked).toBe(false);
    });
  });
});

// --- checkNetworkAccess (异步, DNS 预解析) ---

describe('checkNetworkAccess', () => {
  it('blocks hostname resolving to private IP', async () => {
    const resolver = mockResolver(['10.0.0.5'], new Error('no AAAA'));
    const result = await checkNetworkAccess('evil.example.com', BLOCKED_HOSTS, resolver);
    expect(result.blocked).toBe(true);
    expect(result.reason).toBe('private_ip');
    expect(result.target).toContain('evil.example.com');
    expect(result.target).toContain('10.0.0.5');
  });

  it('blocks hostname resolving to loopback via IPv6', async () => {
    const resolver = mockResolver(new Error('no A'), ['::1']);
    const result = await checkNetworkAccess('sneaky.example.com', BLOCKED_HOSTS, resolver);
    expect(result.blocked).toBe(true);
    expect(result.reason).toBe('loopback');
  });

  it('allows hostname resolving to public IP', async () => {
    const resolver = mockResolver(['142.250.80.46'], new Error('no AAAA'));
    const result = await checkNetworkAccess('google.com', BLOCKED_HOSTS, resolver);
    expect(result.blocked).toBe(false);
  });

  it('allows when DNS resolution fails entirely', async () => {
    const resolver = mockResolver(new Error('ENOTFOUND'), new Error('ENOTFOUND'));
    const result = await checkNetworkAccess('nonexistent.example.com', BLOCKED_HOSTS, resolver);
    expect(result.blocked).toBe(false);
  });

  it('skips DNS for IP addresses', async () => {
    let called = false;
    const resolver: DnsResolver = {
      resolve4: () => { called = true; return Promise.resolve([]); },
      resolve6: () => { called = true; return Promise.resolve([]); },
    };
    const result = await checkNetworkAccess('8.8.8.8', BLOCKED_HOSTS, resolver);
    expect(result.blocked).toBe(false);
    expect(called).toBe(false);
  });

  it('blocks IP addresses directly without DNS', async () => {
    const result = await checkNetworkAccess('10.0.0.1', BLOCKED_HOSTS);
    expect(result.blocked).toBe(true);
    expect(result.reason).toBe('private_ip');
  });
});
