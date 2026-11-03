#!/usr/bin/env node
// Verifies the client wire helpers against the protocol definition. Upstream
// stores an IPv4 address with u32::from_be_bytes (easytier-proto/src/common.rs),
// so the most significant byte is the first octet; a UUID is four big endian
// u32 parts. Getting either backwards produces plausible looking garbage.
import { formatIpv4, uuidParts } from '../client/src/lib/wire.ts';

const cases = [
  { addr: 0x0a636401, length: 24, want: '10.99.100.1/24' },
  { addr: 0x0a000001, length: 8, want: '10.0.0.1/8' },
  { addr: 0xffffffff, length: 32, want: '255.255.255.255/32' },
  { addr: 0x00000000, length: 0, want: '0.0.0.0' },
];

let failures = 0;
// Without cases the loop below proves nothing.
if (cases.length < 4) {
  console.log('wire format: only ' + cases.length + ' case(s); too few format cases to mean anything');
  process.exit(1);
}

for (const testCase of cases) {
  const got = formatIpv4(testCase.addr, testCase.length);
  if (got !== testCase.want) {
    console.log('  formatIpv4(0x' + testCase.addr.toString(16) + ') = ' + got + ', want ' + testCase.want);
    failures += 1;
  }
}

const uuid = uuidParts('9a7c0000-0000-4000-8000-000000000001');
const wantParts = { part1: 0x9a7c0000, part2: 0x00004000, part3: 0x80000000, part4: 0x00000001 };
for (const key of ['part1', 'part2', 'part3', 'part4']) {
  if (uuid[key] !== wantParts[key]) {
    console.log('  uuidParts.' + key + ' = ' + uuid[key] + ', want ' + wantParts[key]);
    failures += 1;
  }
}

if (failures > 0) {
  console.error('wire format: ' + failures + ' mismatch(es)');
  process.exit(1);
}
console.log('wire format: ipv4 and uuid encodings match the protocol definition');
