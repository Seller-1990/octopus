package stream

import (
	"context"
	"io"
)

// RawSource reads raw bytes in fixed-size chunks (for passthrough).
type RawSource struct {
	reader  io.ReadCloser
	bufSize int
}

// NewRawSource creates a source that reads raw chunks.
func NewRawSource(reader io.ReadCloser, bufSize int) *RawSource {
	if bufSize <= 0 {
		bufSize = 32 * 1024 // 32KB default
	}
	return &RawSource{
		reader:  reader,
		bufSize: bufSize,
	}
}

// ReadEvent reads the next chunk of raw bytes.
func (s *RawSource) ReadEvent(ctx context.Context) ([]byte, error) {
	// buf 每次调用都是新分配的，不存在复用冲突，直接切片返回即可——
	// 省去 passthrough 热路径上每 32KB 一次的二次分配与拷贝。
	buf := make([]byte, s.bufSize)
	n, err := s.reader.Read(buf)
	if n > 0 {
		return buf[:n:n], nil
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// Close releases the underlying reader.
func (s *RawSource) Close() error {
	if s.reader != nil {
		return s.reader.Close()
	}
	return nil
}
