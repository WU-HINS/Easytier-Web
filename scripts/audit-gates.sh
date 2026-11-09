#!/usr/bin/env bash
# Every gate item must be able to fail. A check that only ever passes is worse
# than no check. This audit works on a COPY of the repository: it breaks the
# subject of each checker, expects a non zero exit, and never touches the real
# tree (an earlier version mutated in place and left the tree dirty when one
# restore step failed).
set -uo pipefail
cd "$(dirname "$0")/.."
SOURCE="$PWD"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

mkdir -p "$WORK/repo"
(cd "$SOURCE" && tar --exclude=node_modules --exclude=dist --exclude=.toolchain -cf - .) | (cd "$WORK/repo" && tar -xf -)

fail=0
audited=0

audit() {
  local name="$1" file="$2" mutation="$3" checker="$4" cleanup="${5:-}"
  audited=$((audited + 1))
  cp "$WORK/repo/$file" "$WORK/pristine"
  if ! (cd "$WORK/repo" && python3 -c "$mutation"); then
    echo "  SETUP    $name (mutation did not apply)"
    fail=1
    cp "$WORK/pristine" "$WORK/repo/$file"
    return
  fi
  if (cd "$WORK/repo" && eval "$checker") > "$WORK/out" 2>&1; then
    echo "  VACUOUS  $name (checker stayed green after the breakage)"
    fail=1
  else
    # Failing is not enough: it has to fail for the reason the case names.
    # A checker that crashes (missing module, syntax error, nil map) also exits
    # non-zero, and that has already been mistaken for a red proof three times
    # in this project (review team, r134).
    if [ ! -s "$WORK/out" ]; then
      echo "  WRONGFUL $name (checker failed with no output)"
      fail=1
    elif grep -qE '(ERR_MODULE_NOT_FOUND|SyntaxError|ReferenceError|TypeError:|panic:)' "$WORK/out"; then
      echo "  WRONGFUL $name (checker crashed instead of reporting)"
      fail=1
    else
      echo "  OK       $name"
    fi
  fi
  cp "$WORK/pristine" "$WORK/repo/$file"
  if [ -n "$cleanup" ]; then (cd "$WORK/repo" && eval "$cleanup"); fi
  if ! (cd "$WORK/repo" && eval "$checker") > /dev/null 2>&1; then
    echo "  BROKEN   $name (checker still fails after restore)"
    fail=1
  fi
}

echo "== gate audit: every check must be able to fail =="

audit "design tokens" "client/src/ui/tokens.css" "p='client/src/ui/tokens.css'; s=open(p).read(); s=s.replace('--phi: 1.618034;','--phi: 1.5;',1); open(p,'w').write(s)" "node scripts/check-design-tokens.mjs"

audit "design contrast" "client/src/ui/tokens.css" "p='client/src/ui/tokens.css'; s=open(p).read(); s=s.replace('--muted-foreground: 215 20% 40%;','--muted-foreground: 215 20% 97%;',1); open(p,'w').write(s)" "node scripts/check-contrast.mjs"

audit "api contract" "client/src/features/networks/api.ts" "p='client/src/features/networks/api.ts'; s=open(p).read(); s=s.replace(chr(39)+'/networks'+chr(39),chr(39)+'/networksX'+chr(39),1); open(p,'w').write(s)" "node scripts/check-contract.mjs"

audit "wire format" "client/src/lib/wire.ts" "p='client/src/lib/wire.ts'; s=open(p).read(); s=s.replace('offset + 8','offset + 6',1); open(p,'w').write(s)" "node --experimental-strip-types scripts/check-wire.mjs"

audit "docs consistency" "ai-censorship/ledger.md" \
"open('ai-censorship/review-20260101-r999.md','w').write(chr(35)+' orphan record')" \
"node scripts/check-docs.mjs" \
"rm -f ai-censorship/review-20260101-r999.md"

audit "readme commands" "README.md" "p='README.md'; s=open(p).read(); s=s.replace('## 快速开始','## 快速开始'+chr(10)+chr(10)+'--ghost-flag 并不存在',1); open(p,'w').write(s)" "node scripts/check-readme.mjs"

audit "ui states" "client/src/features/dashboard/dashboard-page.tsx" "p='client/src/features/dashboard/dashboard-page.tsx'; s=open(p).read(); probe='const gateAuditProbe = <div dangerouslySetInnerHTML={{ __html: '+chr(39)+chr(39)+' }} />;'+chr(10); s=s.replace('export function',probe+'export function',1); open(p,'w').write(s)" "node scripts/check-ui.mjs"

audit "client origin" "client/src/lib/origin.ts" "p='client/src/lib/origin.ts'; s=open(p).read(); s=s.replace('const { protocol, hostname } = window.location;','const { protocol, hostname } = window.location; return true;',1); open(p,'w').write(s)" "node --experimental-transform-types scripts/check-client-origin.mjs"

audit "client request" "client/src/lib/request.ts" "p='client/src/lib/request.ts'; s=open(p).read(); s=s.replace(chr(39)+'same-origin'+chr(39),chr(39)+'omit'+chr(39),1); open(p,'w').write(s)" "node --experimental-transform-types scripts/check-client-request.mjs"


# Go level invariants: the same idea, one layer down. Each of these guards
# a property an earlier round fixed after a real defect. If any of them can be
# removed without a test going red, the fix is not actually protected.
audit "single admin index" "internal/db/dsm/complete_schema.go" \
"p='internal/db/dsm/complete_schema.go'; s=open(p).read(); s=s.replace(chr(34)+'uq_users_single_admin'+chr(34), chr(34)+'uq_users_single_admin_disabled'+chr(34),1); open(p,'w').write(s)" \
"env -u EASYTIER_TEST_PG_DSN -u EASYTIER_HTTPAPI_PG GOFLAGS=-mod=mod go test -count=1 -run TestSingleAdminIndexExists ./internal/db/"

audit "quota inside the transaction" "internal/db/bal/networks.go" \
"p='internal/db/bal/networks.go'; s=open(p).read(); s=s.replace('if current+len(members) > quota {','if false {',1); open(p,'w').write(s)" \
"env -u EASYTIER_TEST_PG_DSN -u EASYTIER_HTTPAPI_PG GOFLAGS=-mod=mod go test -count=1 -run TestAddMembersBoundedEnforcesTheQuota ./internal/db/"

audit "duplicate username mapping" "internal/db/bal/users.go" \
"p='internal/db/bal/users.go'; s=open(p).read(); s=s.replace('if isUsernameViolation(err) {','if false {',1); open(p,'w').write(s)" \
"env -u EASYTIER_TEST_PG_DSN -u EASYTIER_HTTPAPI_PG GOFLAGS=-mod=mod go test -count=1 -run TestCreateUserReportsDuplicateNames ./internal/db/"
audit "root charter" "root.md" \
"p='root.md'; s=open(p).read(); old='postgres e2e'; assert old in s, 'anchor missing'; open(p,'w').write(s.replace(old, 'postgres end-to-end',1))" \
"node scripts/check-docs.mjs"
audit "dark theme tokens" "client/src/ui/tokens.css" \
"p='client/src/ui/tokens.css'; s=open(p).read(); old='--muted-foreground: 215 20% 70%;'; assert old in s, 'anchor missing'; open(p,'w').write(s.replace(old, '',1))" \
"node scripts/check-contrast.mjs"
audit "read route session" "internal/httpapi/router.go" \
"p='internal/httpapi/router.go'; s=open(p).read(); old='s.requireUser(s.handleListMachines)'; assert old in s, 'anchor missing'; open(p,'w').write(s.replace(old, 's.handleListMachines',1))" \
"env -u EASYTIER_TEST_PG_DSN -u EASYTIER_HTTPAPI_PG GOFLAGS=-mod=mod go test -count=1 -run TestReadRoutesRequireASession ./internal/arch/"
audit "inline copy" "client/src/app/app-shell.tsx" \
"p='client/src/app/app-shell.tsx'; s=open(p).read(); probe=chr(10)+'const gateAuditCjk = '+chr(39)+'新增中文字面量'+chr(39)+';'+chr(10); assert 'export function' in s, 'anchor missing'; open(p,'w').write(s.replace('export function', probe+'export function',1))" \
"node scripts/check-ui.mjs"
audit "editor invariants" "client/src/features/networks/member-editor.tsx" "p='client/src/features/networks/member-editor.tsx'; s=open(p).read(); old='advanceBaseline();'; assert old in s, 'anchor missing'; open(p,'w').write(s.removeprefix('') .replace(old,'',1))" "node scripts/check-editor-invariants.mjs"
echo
if [ "$fail" -ne 0 ]; then
  echo "result: FAIL ($audited checks audited)"
  exit 1
fi
echo "result: PASS ($audited checks, each shown to fail when its subject breaks)"
