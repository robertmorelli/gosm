#!/bin/sh
# Runs the Kotlin and Dart benchmarks on V8 (node) and as gosm-translated Go,
# printing the best of N runs per benchmark (ms, lower is better).
set -e
cd "$(dirname "$0")/.."
N=${N:-3}
go build -o /tmp/gosm-kbench ./bench/kotlin/cmd
go build -o /tmp/gosm-dbench ./bench/dart/cmd
best() { # file -> "name best" lines
  awk '{ if (!($1 in b) || $2 < b[$1]) b[$1] = $2 } END { for (k in b) print k, b[k] }' "$1" | sort
}
run() { # label cmd...
  label=$1; shift
  : > /tmp/gosm-bench.$label
  for i in $(seq "$N"); do "$@" 2>/dev/null | grep " ms " >> /tmp/gosm-bench.$label; done
}
run kv8 sh -c 'cd bench/kotlin && node kbench.mjs'
run kgo /tmp/gosm-kbench
run dv8 sh -c 'cd bench/dart && node run.mjs'
run dgo /tmp/gosm-dbench
printf "%-15s %10s %10s %8s   %10s %10s %8s\n" benchmark "kotlin-v8" "kotlin-go" "speedup" "dart-v8" "dart-go" "speedup"
best /tmp/gosm-bench.kv8 > /tmp/a; best /tmp/gosm-bench.kgo > /tmp/b; best /tmp/gosm-bench.dv8 > /tmp/c; best /tmp/gosm-bench.dgo > /tmp/d
join /tmp/a /tmp/b | join - /tmp/c | join - /tmp/d | awk '{ printf "%-15s %10.1f %10.1f %7.2fx   %10.1f %10.1f %7.2fx\n", $1, $2, $3, $2/$3, $4, $5, $4/$5 }'
