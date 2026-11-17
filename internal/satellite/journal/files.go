package journal

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	epochFileName = "epoch"
	ackFileName   = "ack"
	segmentPrefix = "seg-"
	segmentSuffix = ".log"
	// frameHeader is the length and the CRC32C, 4 bytes each.
	frameHeader = 8
	// maxRecordBytes rejects a corrupt length prefix before allocating it.
	maxRecordBytes = 16 << 20
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

type segment struct {
	path        string
	first, last int64
	size        int64
}

type ackFile struct {
	Epoch   int64 `json:"epoch"`
	Acked   int64 `json:"acked"`
	Floor   int64 `json:"floor"`
	LastSeq int64 `json:"last_seq"`
}

func segmentPath(dir string, first int64) string {
	return filepath.Join(dir, fmt.Sprintf("%s%020d%s", segmentPrefix, first, segmentSuffix))
}

func encodeRecord(r Record) ([]byte, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode journal record: %w", err)
	}
	frame := make([]byte, frameHeader+len(body))
	binary.BigEndian.PutUint32(frame[0:4], uint32(len(body)))
	binary.BigEndian.PutUint32(frame[4:8], crc32.Checksum(body, castagnoli))
	copy(frame[frameHeader:], body)
	return frame, nil
}

// readSegment returns the segment's intact records and the byte offset
// after the last one; whatever follows is torn or corrupt.
func readSegment(path string) ([]Record, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open journal segment: %w", err)
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	var (
		records []Record
		offset  int64
		header  [frameHeader]byte
	)
	for {
		if _, err := io.ReadFull(r, header[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return records, offset, nil
			}
			return nil, 0, fmt.Errorf("read journal segment: %w", err)
		}
		size := binary.BigEndian.Uint32(header[0:4])
		if size == 0 || size > maxRecordBytes {
			return records, offset, nil
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(r, body); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return records, offset, nil
			}
			return nil, 0, fmt.Errorf("read journal segment: %w", err)
		}
		if crc32.Checksum(body, castagnoli) != binary.BigEndian.Uint32(header[4:8]) {
			return records, offset, nil
		}
		var rec Record
		if err := json.Unmarshal(body, &rec); err != nil {
			return records, offset, nil
		}
		records = append(records, rec)
		offset += int64(frameHeader) + int64(size)
	}
}

// loadSegments lists the segments in seq order and cuts a torn tail off
// each of them.
func loadSegments(dir string) ([]*segment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read journal directory: %w", err)
	}
	var segs []*segment
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, segmentPrefix) || !strings.HasSuffix(name, segmentSuffix) {
			continue
		}
		first, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(name, segmentPrefix), segmentSuffix), 10, 64)
		if err != nil {
			continue
		}
		segs = append(segs, &segment{path: filepath.Join(dir, name), first: first})
	}
	sort.Slice(segs, func(a, b int) bool { return segs[a].first < segs[b].first })
	for _, seg := range segs {
		records, offset, err := readSegment(seg.path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(seg.path)
		if err != nil {
			return nil, fmt.Errorf("stat journal segment: %w", err)
		}
		if info.Size() != offset {
			if err := os.Truncate(seg.path, offset); err != nil {
				return nil, fmt.Errorf("truncate torn journal segment: %w", err)
			}
		}
		seg.size = offset
		seg.last = seg.first - 1
		if n := len(records); n > 0 {
			seg.last = records[n-1].Seq
		}
	}
	return segs, nil
}

func readEpoch(dir string) (int64, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, epochFileName))
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read journal epoch: %w", err)
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse journal epoch: %w", err)
	}
	return epoch, true, nil
}

func writeEpoch(dir string, epoch int64) error {
	return writeAtomic(dir, epochFileName, []byte(strconv.FormatInt(epoch, 10)+"\n"))
}

func readAck(dir string) (ackFile, error) {
	data, err := os.ReadFile(filepath.Join(dir, ackFileName))
	if errors.Is(err, os.ErrNotExist) {
		return ackFile{}, nil
	}
	if err != nil {
		return ackFile{}, fmt.Errorf("read journal ack: %w", err)
	}
	var ack ackFile
	if err := json.Unmarshal(data, &ack); err != nil {
		return ackFile{}, fmt.Errorf("parse journal ack: %w", err)
	}
	return ack, nil
}

func writeAck(dir string, ack ackFile) error {
	data, err := json.Marshal(ack)
	if err != nil {
		return fmt.Errorf("encode journal ack: %w", err)
	}
	return writeAtomic(dir, ackFileName, data)
}

func writeAtomic(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "."+name+"-*")
	if err != nil {
		return fmt.Errorf("write journal %s: %w", name, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write journal %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync journal %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close journal %s: %w", name, err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("replace journal %s: %w", name, err)
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open journal directory: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync journal directory: %w", err)
	}
	return nil
}
