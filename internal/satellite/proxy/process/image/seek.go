package image

import (
	"errors"
	"io"
)

// reopeningReader supplies random access for ServeContent without buffering a
// registry blob in memory. Backward seeks reopen the source stream.
type reopeningReader struct {
	body           io.ReadCloser
	open           func() (io.ReadCloser, error)
	size           int64
	position       int64
	readerPosition int64
}

func (r *reopeningReader) Read(p []byte) (int, error) {
	if r.position >= r.size {
		return 0, io.EOF
	}
	if int64(len(p)) > r.size-r.position {
		p = p[:int(r.size-r.position)]
	}
	if r.position < r.readerPosition {
		if err := r.body.Close(); err != nil {
			return 0, err
		}
		body, err := r.open()
		if err != nil {
			return 0, err
		}
		r.body = body
		r.readerPosition = 0
	}
	if r.position > r.readerPosition {
		skipped, err := io.CopyN(io.Discard, r.body, r.position-r.readerPosition)
		r.readerPosition += skipped
		if err != nil {
			return 0, err
		}
	}
	n, err := r.body.Read(p)
	r.position += int64(n)
	r.readerPosition += int64(n)
	return n, err
}

func (r *reopeningReader) Seek(offset int64, whence int) (int64, error) {
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = r.position
	case io.SeekEnd:
		base = r.size
	default:
		return 0, errors.New("invalid seek whence")
	}
	position := base + offset
	if position < 0 || (offset > 0 && position < base) || (offset < 0 && position > base) {
		return 0, errors.New("invalid seek position")
	}
	r.position = position
	return position, nil
}

func (r *reopeningReader) Close() error {
	return r.body.Close()
}
