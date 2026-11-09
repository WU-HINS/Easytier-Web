/**
 * Pure helpers from the wire layer. Everything that needs a browser (the pages, the
 * forms, the language switch) is covered by scripts/ui-tour.mjs against a running
 * console instead; this file only pins logic that can be checked in isolation.
 */
import { describe, expect, it } from 'vitest';
import { formatIpv4, uuidParts } from './wire';

describe('uuidParts', () => {
  it('splits a uuid into the four 32-bit words the RPC message wants', () => {
    const parts = uuidParts('11111111-2222-3333-4444-555555555555');
    expect(parts).toEqual({ part1: 0x11111111, part2: 0x22223333, part3: 0x44445555, part4: 0x55555555 });
  });

  it('treats the words as unsigned so a high bit does not become a negative number', () => {
    const parts = uuidParts('ffffffff-ffff-ffff-ffff-ffffffffffff');
    expect(parts.part1).toBe(0xffffffff);
    expect(parts.part1).toBeGreaterThan(0);
  });
});

describe('formatIpv4', () => {
  it('renders dotted quad with a prefix length', () => {
    expect(formatIpv4(0x0a0b0c01, 24)).toBe('10.11.12.1/24');
  });

  it('omits the prefix when there is none', () => {
    expect(formatIpv4(0x0a0b0c01, undefined)).toBe('10.11.12.1');
  });

  it('renders an em dash when the address is absent', () => {
    expect(formatIpv4(undefined, 24)).toBe('—');
  });
});
