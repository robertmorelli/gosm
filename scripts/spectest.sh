#!/bin/sh
# Downloads the WebAssembly spec test suite, converts it to JSON with
# wasm-tools (and wabt for the legacy exception tests), compiles every module
# with gosm and runs all assertions.
#
#   scripts/spectest.sh [-run regexp] [-v]
#
# Requires: git, wasm-tools, wast2json (wabt), go.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${GOSM_SPECTEST_DIR:-$ROOT/internal/spectest/_work}
mkdir -p "$WORK"
if [ ! -d "$WORK/spec" ]; then
  git clone --depth 1 https://github.com/WebAssembly/spec.git "$WORK/spec"
fi
OUT=$WORK/json
if [ ! -d "$OUT" ]; then
  mkdir -p "$OUT" "$WORK/patched"
  # wasm-tools rejects one assert_invalid at parse time; drop it.
  python3 - "$WORK" <<'PY'
import sys
w = sys.argv[1]
s = open(w + "/spec/test/core/gc/type-subtyping.wast").read()
i = s.index(";; Testing the number of allowed supertypes")
j = s.index(";; Testing exported functions with non-flat types")
open(w + "/patched/type-subtyping.wast", "w").write(s[:i] + s[j:])
PY
  cd "$WORK/spec/test/core"
  for f in $(find . -name '*.wast'); do
    n=$(echo "$f" | sed 's|^\./||; s|/|__|g; s|\.wast$||')
    src=$f
    [ "$n" = "gc__type-subtyping" ] && src=$WORK/patched/type-subtyping.wast
    mkdir -p "$OUT/$n"
    wasm-tools json-from-wast "$src" -o "$OUT/$n/test.json" --wasm-dir "$OUT/$n/" || echo "skip $n"
  done
  cd "$WORK/spec/test/legacy/exceptions/core"
  for f in *.wast; do
    n=legacy__${f%.wast}
    mkdir -p "$OUT/$n"
    (cd "$OUT/$n" && wast2json --enable-exceptions --enable-tail-call "$WORK/spec/test/legacy/exceptions/core/$f" -o test.json)
  done
fi
# gosm's own extra tests (js-string builtins, function subtyping views, ...)
for f in "$ROOT"/internal/spectest/extra/*.wast; do
  n=extra__$(basename "$f" .wast)
  rm -rf "${OUT:?}/$n"
  mkdir -p "$OUT/$n"
  wasm-tools json-from-wast "$f" -o "$OUT/$n/test.json" --wasm-dir "$OUT/$n/"
done
cd "$ROOT"
go run ./internal/spectest/gen -wast "$OUT" "$@"
