package compiler

import "gosm/internal/wasm"

// computeRecursive marks the functions that can take part in unbounded
// recursion: those in a cycle of the (conservative) call graph. Only they
// need the call-depth check, so small leaf functions stay inlinable.
//
// Indirect calls (call_indirect, call_ref and their tail variants) may reach
// any function whose address is taken; calls to imports may re-enter the
// module through any export.
func (c *compiler) computeRecursive() {
	m := c.m
	n := m.NumFuncs()
	nimp := m.NumImportedFuncs
	var addrTaken, exported []int
	for f := range c.refFuncs {
		addrTaken = append(addrTaken, int(f))
	}
	for f := range c.exportFns {
		exported = append(exported, int(f))
	}
	edges := make([][]int, n)
	for i := 0; i < nimp; i++ {
		if _, ok := c.builtin[uint32(i)]; ok {
			continue // builtins never call back into the module
		}
		edges[i] = append(append(edges[i], exported...), addrTaken...)
	}
	for i, code := range m.Codes {
		f := nimp + i
		indirect := false
		for _, in := range code.Body {
			switch in.Op {
			case wasm.OpCall, wasm.OpReturnCall:
				edges[f] = append(edges[f], int(in.A))
			case wasm.OpCallIndirect, wasm.OpReturnCallIndirect, wasm.OpCallRef, wasm.OpReturnCallRef:
				indirect = true
			}
		}
		if indirect {
			edges[f] = append(edges[f], addrTaken...)
		}
	}
	// Tarjan's SCC algorithm (iterative).
	index := make([]int, n)
	low := make([]int, n)
	onStack := make([]bool, n)
	for i := range index {
		index[i] = -1
	}
	c.recursive = make([]bool, n)
	var stack []int
	next := 0
	type frame struct{ v, ei int }
	for s := 0; s < n; s++ {
		if index[s] >= 0 {
			continue
		}
		work := []frame{{s, 0}}
		index[s], low[s] = next, next
		next++
		stack = append(stack, s)
		onStack[s] = true
		for len(work) > 0 {
			fr := &work[len(work)-1]
			v := fr.v
			if fr.ei < len(edges[v]) {
				w := edges[v][fr.ei]
				fr.ei++
				if index[w] < 0 {
					index[w], low[w] = next, next
					next++
					stack = append(stack, w)
					onStack[w] = true
					work = append(work, frame{w, 0})
				} else if onStack[w] && index[w] < low[v] {
					low[v] = index[w]
				}
				continue
			}
			work = work[:len(work)-1]
			if len(work) > 0 {
				p := work[len(work)-1].v
				if low[v] < low[p] {
					low[p] = low[v]
				}
			}
			if low[v] == index[v] {
				var comp []int
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					comp = append(comp, w)
					if w == v {
						break
					}
				}
				if len(comp) > 1 {
					for _, w := range comp {
						c.recursive[w] = true
					}
				} else {
					for _, w := range edges[v] {
						if w == v {
							c.recursive[v] = true
						}
					}
				}
			}
		}
	}
}

// computeTailCalls decides how tail calls are lowered. Direct tail calls to
// functions outside the caller's tail-call cycle are bounded and become
// plain Go calls; self tail calls become loops. Tail calls that can recur
// without bound (direct calls within a tail-call cycle, indirect tail calls,
// and tail calls inside exception handlers) use a trampoline: the pending
// call is stored in m.tail and run by the caller's loop. usesTramp enables
// the trampoline checks after calls.
func (c *compiler) computeTailCalls() {
	m := c.m
	n := m.NumFuncs()
	nimp := m.NumImportedFuncs
	edges := make([][]int, n)
	for i, code := range m.Codes {
		f := nimp + i
		depth := 0
		for _, in := range code.Body {
			switch in.Op {
			case wasm.OpBlock, wasm.OpLoop, wasm.OpIf:
				depth <<= 1
			case wasm.OpTry, wasm.OpTryTable:
				depth = depth<<1 | 1
			case wasm.OpEnd, wasm.OpDelegate:
				depth >>= 1
			case wasm.OpReturnCallIndirect, wasm.OpReturnCallRef:
				c.usesTramp = true
			case wasm.OpReturnCall:
				if depth != 0 {
					c.usesTramp = true
				}
				if int(in.A) != f {
					edges[f] = append(edges[f], int(in.A))
				}
			}
		}
	}
	comp, cyclic := sccs(n, edges)
	c.tailComp = comp
	c.tailCyclic = cyclic
	for f := range edges {
		for _, g := range edges[f] {
			if comp[f] == comp[g] && cyclic[comp[f]] {
				c.usesTramp = true
			}
		}
	}
}

// inTailCycle reports whether a direct tail call f -> g may recur unboundedly.
func (c *compiler) inTailCycle(f, g int) bool {
	return c.tailComp[f] == c.tailComp[g] && c.tailCyclic[c.tailComp[f]]
}

// sccs returns the strongly connected component of every node and whether
// each component contains more than one node.
func sccs(n int, edges [][]int) (comp []int, multi map[int]bool) {
	index := make([]int, n)
	low := make([]int, n)
	onStack := make([]bool, n)
	comp = make([]int, n)
	multi = map[int]bool{}
	for i := range index {
		index[i] = -1
	}
	var stack []int
	next, ncomp := 0, 0
	type frame struct{ v, ei int }
	for s := 0; s < n; s++ {
		if index[s] >= 0 {
			continue
		}
		work := []frame{{s, 0}}
		index[s], low[s] = next, next
		next++
		stack = append(stack, s)
		onStack[s] = true
		for len(work) > 0 {
			fr := &work[len(work)-1]
			v := fr.v
			if fr.ei < len(edges[v]) {
				w := edges[v][fr.ei]
				fr.ei++
				if index[w] < 0 {
					index[w], low[w] = next, next
					next++
					stack = append(stack, w)
					onStack[w] = true
					work = append(work, frame{w, 0})
				} else if onStack[w] && index[w] < low[v] {
					low[v] = index[w]
				}
				continue
			}
			work = work[:len(work)-1]
			if len(work) > 0 {
				p := work[len(work)-1].v
				if low[v] < low[p] {
					low[p] = low[v]
				}
			}
			if low[v] == index[v] {
				size := 0
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					comp[w] = ncomp
					size++
					if w == v {
						break
					}
				}
				if size > 1 {
					multi[ncomp] = true
				}
				ncomp++
			}
		}
	}
	return comp, multi
}
