#!/usr/bin/env node
// Client side security rule from G7: the OIDC callback is produced by the browser
// and binding is only offered on a secure origin. The helpers read
// window.location at call time, so they can be exercised here with a stub.
const problems = [];
const NL = String.fromCharCode(10);

const load = async (location) => {
  globalThis.window = { location };
  const module = await import('../client/src/lib/origin.ts');
  return module;
};

const cases = [
  { name: 'https console', location: { protocol: 'https:', hostname: 'console.example', origin: 'https://console.example' }, secure: true },
  { name: 'http localhost', location: { protocol: 'http:', hostname: 'localhost', origin: 'http://localhost:11211' }, secure: true },
  { name: 'http loopback', location: { protocol: 'http:', hostname: '127.0.0.1', origin: 'http://127.0.0.1:11211' }, secure: true },
  { name: 'http ipv6 loopback', location: { protocol: 'http:', hostname: '[::1]', origin: 'http://[::1]:11211' }, secure: true },
  { name: 'http public host', location: { protocol: 'http:', hostname: 'console.example', origin: 'http://console.example' }, secure: false },
];

for (const testCase of cases) {
  const module = await load(testCase.location);
  const secure = module.isSecureOrigin();
  if (secure !== testCase.secure) {
    problems.push(testCase.name + ': isSecureOrigin = ' + secure + ', want ' + testCase.secure);
  }
  const callback = module.oidcCallbackURL();
  const wanted = testCase.location.origin + '/api/v1/auth/oidc/callback';
  if (callback !== wanted) {
    problems.push(testCase.name + ': callback = ' + callback + ', want ' + wanted);
  }
  const absoluteUrl = module.absolute('/auth/oidc/login');
  if (absoluteUrl !== testCase.location.origin + '/api/v1/auth/oidc/login') {
    problems.push(testCase.name + ': absolute() = ' + absoluteUrl);
  }
}

if (problems.length === 0) {
  console.log('client origin: ' + cases.length + ' origins checked, callback stays on the browser origin');
  process.exit(0);
}
console.log('client origin: ' + problems.length + ' problem(s)');
for (const problem of problems) console.log('  ' + problem);
process.exit(1);
