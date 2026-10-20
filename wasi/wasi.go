// Package wasi implements WASI preview1 (wasi_snapshot_preview1) host
// functions for gosm-generated packages.
//
// Its methods follow gosm's Host naming convention, so *wasi.Host satisfies
// (the WASI part of) any generated Host interface. Embed it in your own host
// type to add other imports:
//
//	type host struct{ *wasi.Host }
//	h := &host{wasi.New(os.Args, nil)}
//	inst, _ := mymod.New(h)
//	h.Memory = inst.Memory()
//	err := h.Run(inst.Start)
package wasi

import (
	"crypto/rand"
	"encoding/binary"
	"io"
	"os"
	"time"

	"gosm/rt"
)

// Errno values.
const (
	ErrnoSuccess = 0
	ErrnoBadf    = 8
	ErrnoInval   = 28
	ErrnoIO      = 29
	ErrnoNosys   = 52
	ErrnoSpipe   = 70
	ErrnoNotsup  = 58
)

// Exit is panicked by proc_exit.
type Exit struct{ Code int32 }

func (e *Exit) Error() string { return "wasi exit" }

// Host provides WASI preview1 functions. Set Memory to the instance's
// exported memory after instantiation.
type Host struct {
	Memory *rt.Memory
	Args   []string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	start  time.Time
}

// New creates a host with the given arguments and environment (nil env
// means no variables).
func New(args, env []string) *Host {
	return &Host{Args: args, Env: env, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, start: time.Now()}
}

// Run calls f (typically the module's _start export) and returns the exit
// code requested via proc_exit (0 if f returns normally) or a wasm error.
func (h *Host) Run(f func()) (code int32, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*Exit); ok {
				code, err = e.Code, nil
				return
			}
			if e := rt.AsError(r); e != nil {
				code, err = 1, e
				return
			}
			panic(r)
		}
	}()
	f()
	return 0, nil
}

func (h *Host) mem() []byte { return h.Memory.Data }

func (h *Host) u32(p int32) uint32 { return binary.LittleEndian.Uint32(h.mem()[uint32(p):]) }

func (h *Host) putU32(p int32, v uint32) { binary.LittleEndian.PutUint32(h.mem()[uint32(p):], v) }

func (h *Host) putU64(p int32, v uint64) { binary.LittleEndian.PutUint64(h.mem()[uint32(p):], v) }

func strsSizes(h *Host, list []string, countPtr, sizePtr int32) int32 {
	n := 0
	for _, s := range list {
		n += len(s) + 1
	}
	h.putU32(countPtr, uint32(len(list)))
	h.putU32(sizePtr, uint32(n))
	return ErrnoSuccess
}

func strsGet(h *Host, list []string, ptrs, buf int32) int32 {
	m := h.mem()
	off := uint32(buf)
	for i, s := range list {
		h.putU32(ptrs+int32(4*i), off)
		copy(m[off:], s)
		m[off+uint32(len(s))] = 0
		off += uint32(len(s)) + 1
	}
	return ErrnoSuccess
}

func (h *Host) M_wasi_snapshot_preview1_args_sizes_get(countPtr, sizePtr int32) int32 {
	return strsSizes(h, h.Args, countPtr, sizePtr)
}

func (h *Host) M_wasi_snapshot_preview1_args_get(ptrs, buf int32) int32 {
	return strsGet(h, h.Args, ptrs, buf)
}

func (h *Host) M_wasi_snapshot_preview1_environ_sizes_get(countPtr, sizePtr int32) int32 {
	return strsSizes(h, h.Env, countPtr, sizePtr)
}

func (h *Host) M_wasi_snapshot_preview1_environ_get(ptrs, buf int32) int32 {
	return strsGet(h, h.Env, ptrs, buf)
}

func (h *Host) now(id int32) uint64 {
	if id == 0 { // realtime
		return uint64(time.Now().UnixNano())
	}
	return uint64(time.Since(h.start).Nanoseconds())
}

func (h *Host) M_wasi_snapshot_preview1_clock_res_get(id int32, resPtr int32) int32 {
	h.putU64(resPtr, 1000)
	return ErrnoSuccess
}

func (h *Host) M_wasi_snapshot_preview1_clock_time_get(id int32, precision int64, timePtr int32) int32 {
	h.putU64(timePtr, h.now(id))
	return ErrnoSuccess
}

func (h *Host) writer(fd int32) io.Writer {
	switch fd {
	case 1:
		return h.Stdout
	case 2:
		return h.Stderr
	}
	return nil
}

func (h *Host) M_wasi_snapshot_preview1_fd_write(fd, iovs, iovsLen, nwrittenPtr int32) int32 {
	w := h.writer(fd)
	if w == nil {
		return ErrnoBadf
	}
	m := h.mem()
	total := 0
	for i := int32(0); i < iovsLen; i++ {
		p := h.u32(iovs + 8*i)
		n := h.u32(iovs + 8*i + 4)
		k, err := w.Write(m[p : p+n])
		total += k
		if err != nil {
			h.putU32(nwrittenPtr, uint32(total))
			return ErrnoIO
		}
	}
	h.putU32(nwrittenPtr, uint32(total))
	return ErrnoSuccess
}

func (h *Host) M_wasi_snapshot_preview1_fd_read(fd, iovs, iovsLen, nreadPtr int32) int32 {
	if fd != 0 || h.Stdin == nil {
		return ErrnoBadf
	}
	m := h.mem()
	total := 0
	for i := int32(0); i < iovsLen; i++ {
		p := h.u32(iovs + 8*i)
		n := h.u32(iovs + 8*i + 4)
		k, err := h.Stdin.Read(m[p : p+n])
		total += k
		if err != nil || uint32(k) < n {
			break
		}
	}
	h.putU32(nreadPtr, uint32(total))
	return ErrnoSuccess
}

func (h *Host) M_wasi_snapshot_preview1_fd_close(fd int32) int32 { return ErrnoSuccess }

func (h *Host) M_wasi_snapshot_preview1_fd_seek(fd int32, offset int64, whence int32, newOffsetPtr int32) int32 {
	return ErrnoSpipe
}

func (h *Host) M_wasi_snapshot_preview1_fd_fdstat_get(fd int32, statPtr int32) int32 {
	if fd > 2 {
		return ErrnoBadf
	}
	m := h.mem()
	for i := 0; i < 24; i++ {
		m[uint32(statPtr)+uint32(i)] = 0
	}
	m[uint32(statPtr)] = 2 // character device
	h.putU64(statPtr+8, ^uint64(0))
	h.putU64(statPtr+16, ^uint64(0))
	return ErrnoSuccess
}

func (h *Host) M_wasi_snapshot_preview1_fd_prestat_get(fd int32, bufPtr int32) int32 {
	return ErrnoBadf
}

func (h *Host) M_wasi_snapshot_preview1_fd_prestat_dir_name(fd, path, pathLen int32) int32 {
	return ErrnoBadf
}

func (h *Host) M_wasi_snapshot_preview1_proc_exit(code int32) {
	panic(&Exit{Code: code})
}

func (h *Host) M_wasi_snapshot_preview1_random_get(buf, n int32) int32 {
	rand.Read(h.mem()[uint32(buf) : uint32(buf)+uint32(n)])
	return ErrnoSuccess
}

func (h *Host) M_wasi_snapshot_preview1_sched_yield() int32 { return ErrnoSuccess }

// poll_oneoff supports clock subscriptions (sleeps); fd subscriptions
// report immediate readiness.
func (h *Host) M_wasi_snapshot_preview1_poll_oneoff(in, out, nsubs, neventsPtr int32) int32 {
	m := h.mem()
	var sleep time.Duration
	for i := int32(0); i < nsubs; i++ {
		s := uint32(in) + uint32(48*i)
		userdata := binary.LittleEndian.Uint64(m[s:])
		tag := m[s+8]
		e := uint32(out) + uint32(32*i)
		for k := uint32(0); k < 32; k++ {
			m[e+k] = 0
		}
		binary.LittleEndian.PutUint64(m[e:], userdata)
		m[e+10] = tag
		if tag == 0 { // clock
			timeout := binary.LittleEndian.Uint64(m[s+24:])
			flags := binary.LittleEndian.Uint16(m[s+40:])
			d := time.Duration(timeout)
			if flags&1 != 0 { // absolute
				d = time.Duration(timeout - h.now(int32(binary.LittleEndian.Uint32(m[s+16:]))))
			}
			if d > sleep {
				sleep = d
			}
		}
	}
	if sleep > 0 {
		time.Sleep(sleep)
	}
	h.putU32(neventsPtr, uint32(nsubs))
	return ErrnoSuccess
}
