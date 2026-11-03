/**
 * Wire format helpers. Kept dependency free so the gate can import this file
 * directly (node --experimental-strip-types) and check it against the protocol
 * definition instead of trusting the UI.
 */

/**
 * Split a canonical UUID into the four 32 bit parts the RPC messages use.
 * EasyTier writes them with u32::from_be_bytes over the 16 canonical bytes.
 */
export function uuidParts(value: string): { part1: number; part2: number; part3: number; part4: number } {
  const hex = value.replace(/-/g, '');
  const read = (offset: number) => parseInt(hex.slice(offset, offset + 8), 16) >>> 0;
  return { part1: read(0), part2: read(8), part3: read(16), part4: read(24) };
}

/**
 * Render an Ipv4Inet as dotted quad / prefix. Upstream stores the address with
 * u32::from_be_bytes(octets), so the first octet is the most significant byte:
 * 10.99.100.1 is 0x0A636401.
 */
export function formatIpv4(addr: number | undefined, length: number | undefined): string {
  if (addr === undefined) return '—';
  const octet = (shift: number) => (addr >>> shift) & 255;
  const dotted = octet(24) + '.' + octet(16) + '.' + octet(8) + '.' + octet(0);
  return length ? dotted + '/' + length : dotted;
}
