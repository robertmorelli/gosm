#!/bin/sh
# Rebuild ktapp.wasm from main.kt with the Kotlin/Wasm compiler (WASI target).
set -e
K=$(dirname "$(readlink -f "$(command -v kotlinc)")")/../libexec
STD=$K/lib/kotlin-stdlib-wasm-wasi.klib
rm -rf /tmp/ktklib /tmp/ktout
"$K/bin/kotlinc-wasm" -Xwasm-target=wasm-wasi -nopack -libraries "$STD" -ir-output-dir /tmp/ktklib -ir-output-name ktapp main.kt
"$K/bin/kotlinc-wasm" -Xwasm-target=wasm-wasi -Xir-produce-js -libraries "$STD" -Xinclude=/tmp/ktklib \
  -ir-output-dir /tmp/ktout -ir-output-name ktapp -Xir-dce -Xwasm-use-new-exception-proposal
cp /tmp/ktout/ktapp.wasm .
