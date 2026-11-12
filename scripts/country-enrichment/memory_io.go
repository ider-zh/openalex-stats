package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/checkpoint"
)

// A byte budget bounds whole-file buffers independently of the number of
// decoding workers. Sequential disk reads also supply the full raw SHA256;
// projected Parquet pages are then decoded from immutable in-memory bytes.
type byteBudget struct {
	mu        sync.Mutex
	used, max uint64
	changed   chan struct{}
}

func newByteBudget(n uint64) *byteBudget { return &byteBudget{max: n, changed: make(chan struct{})} }
func (b *byteBudget) acquire(ctx context.Context, n uint64) error {
	if n > b.max {
		return fmt.Errorf("raw source larger than buffer budget")
	}
	for {
		b.mu.Lock()
		if b.used+n <= b.max {
			b.used += n
			b.mu.Unlock()
			return nil
		}
		changed := b.changed
		b.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (b *byteBudget) release(n uint64) {
	b.mu.Lock()
	b.used -= n
	close(b.changed)
	b.changed = make(chan struct{})
	b.mu.Unlock()
}
func (e *Env) bufferSource(ctx context.Context, path string, n int64) ([]byte, checkpoint.Artifact, func(), error) {
	if n < 0 || uint64(n) > uint64(^uint(0)>>1) {
		return nil, checkpoint.Artifact{}, nil, fmt.Errorf("invalid source size")
	}
	if er := e.buffers.acquire(ctx, uint64(n)); er != nil {
		return nil, checkpoint.Artifact{}, nil, er
	}
	release := func() { e.buffers.release(uint64(n)) }
	select {
	case e.readers <- struct{}{}:
	case <-ctx.Done():
		release()
		return nil, checkpoint.Artifact{}, nil, ctx.Err()
	}
	f, er := os.Open(path)
	if er != nil {
		<-e.readers
		release()
		return nil, checkpoint.Artifact{}, nil, er
	}
	buf := make([]byte, int(n))
	_, er = io.ReadFull(f, buf)
	if er == nil {
		var tail [1]byte
		if m, x := f.Read(tail[:]); m != 0 || x != io.EOF {
			er = fmt.Errorf("source grew during buffered read")
		}
	}
	_ = f.Close()
	<-e.readers
	if er != nil {
		release()
		return nil, checkpoint.Artifact{}, nil, er
	}
	sum := sha256.Sum256(buf)
	return buf, checkpoint.Artifact{Path: path, Bytes: n, SHA256: hex.EncodeToString(sum[:])}, release, nil
}
func eachMemory[T any](data []byte, fn func(T) error) error {
	return eachAt(bytes.NewReader(data), int64(len(data)), fn)
}
func eachAt[T any](f io.ReaderAt, size int64, fn func(T) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("parquet projection: %v", p)
		}
	}()
	pf, e := parquet.OpenFile(f, size, parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true))
	if e != nil {
		return e
	}
	r := parquet.NewGenericReader[T](pf)
	defer r.Close()
	buf := make([]T, 256)
	for {
		n, e := r.Read(buf)
		for _, row := range buf[:n] {
			if x := fn(row); x != nil {
				return x
			}
		}
		clear(buf[:n])
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
	}
}
