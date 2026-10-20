import * as m from './dbench.mjs';
import { readFileSync } from 'fs';
const app = await m.compile(readFileSync('./dbench.wasm'));
const inst = await app.instantiate({});
inst.invokeMain();
