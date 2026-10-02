package download

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	mp4 "github.com/abema/go-mp4"
	"github.com/at-wat/ebml-go"
	"github.com/at-wat/ebml-go/webm"
)

const playbackIndexPageSize = 64 * 1024

// PlaybackIndex describes inclusive byte ranges for a DASH SegmentBase source.
type PlaybackIndex struct {
	InitializationStart int64
	InitializationEnd   int64
	IndexStart          int64
	IndexEnd            int64
}

// ProbePlaybackIndex reads container metadata through small HTTP ranges. Media
// payloads are skipped by the container reader rather than downloaded.
func ProbePlaybackIndex(ctx context.Context, client *http.Client, format PlaybackFormat) (PlaybackIndex, error) {
	reader := &playbackRangeReader{ctx: ctx, client: client, format: format, pageSize: playbackIndexPageSize}
	if client == nil {
		reader.client = http.DefaultClient
	}
	if chunk := PlaybackHTTPChunkSize(format.DownloaderOptions); chunk > 0 && chunk < reader.pageSize {
		reader.pageSize = chunk
	}
	page, size, err := reader.readRange(0, reader.pageSize-1)
	if err != nil {
		return PlaybackIndex{}, err
	}
	reader.size = size
	reader.page = page
	switch strings.ToLower(format.Ext) {
	case "mp4", "m4a":
		return probeMP4PlaybackIndex(reader)
	case "webm":
		return probeWebMPlaybackIndex(reader)
	default:
		return PlaybackIndex{}, errors.New("source container has no supported playback index")
	}
}

// PlaybackHTTPChunkSize reads the extractor's requested HTTP chunk size.
func PlaybackHTTPChunkSize(options map[string]any) int64 {
	value := options["http_chunk_size"]
	switch value := value.(type) {
	case float64:
		return int64(value)
	case int:
		return int64(value)
	case int64:
		return value
	case json.Number:
		size, _ := strconv.ParseInt(string(value), 10, 64)
		return size
	case string:
		size, _ := strconv.ParseInt(value, 10, 64)
		return size
	}
	return 0
}

type playbackRangeReader struct {
	ctx      context.Context
	client   *http.Client
	format   PlaybackFormat
	size     int64
	position int64
	pageSize int64
	pageAt   int64
	page     []byte
}

func (r *playbackRangeReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.position >= r.size {
		return 0, io.EOF
	}
	if len(r.page) == 0 || r.position < r.pageAt || r.position >= r.pageAt+int64(len(r.page)) {
		end := min(r.position+r.pageSize-1, r.size-1)
		page, size, err := r.readRange(r.position, end)
		if err != nil {
			return 0, err
		}
		if size != r.size {
			return 0, errors.New("playback source changed during index reading")
		}
		r.page, r.pageAt = page, r.position
	}
	n := copy(p, r.page[r.position-r.pageAt:])
	r.position += int64(n)
	return n, nil
}

func (r *playbackRangeReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.position
	case io.SeekEnd:
		offset += r.size
	default:
		return r.position, errors.New("invalid playback index seek")
	}
	if offset < 0 || offset > r.size {
		return r.position, errors.New("playback index seek is outside the source")
	}
	r.position = offset
	return offset, nil
}

func (r *playbackRangeReader) readRange(start, end int64) ([]byte, int64, error) {
	request, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.format.URL, nil)
	if err != nil {
		return nil, 0, errors.New("invalid playback source URL")
	}
	for name, value := range r.format.Headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	request.Header.Set("Accept-Encoding", "identity")
	response, err := r.client.Do(request)
	if err != nil {
		if r.ctx.Err() != nil {
			return nil, 0, r.ctx.Err()
		}
		return nil, 0, errors.New("playback index request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusPartialContent {
		return nil, 0, fmt.Errorf("playback index request returned HTTP %d", response.StatusCode)
	}
	var actualStart, actualEnd, size int64
	if count, err := fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &actualStart, &actualEnd, &size); err != nil || count != 3 || actualStart != start || actualEnd < actualStart || actualEnd > end || size <= actualEnd {
		return nil, 0, errors.New("playback source returned an invalid byte range")
	}
	length := actualEnd - actualStart + 1
	data, err := io.ReadAll(io.LimitReader(response.Body, length+1))
	if err != nil {
		if r.ctx.Err() != nil {
			return nil, 0, r.ctx.Err()
		}
		return nil, 0, errors.New("could not read playback index bytes")
	}
	if int64(len(data)) != length {
		return nil, 0, errors.New("playback source returned incomplete index bytes")
	}
	return data, size, nil
}

func probeMP4PlaybackIndex(reader *playbackRangeReader) (PlaybackIndex, error) {
	var index PlaybackIndex
	hasFileType, hasMovie, hasIndex := false, false, false
	for reader.position < reader.size {
		box, err := mp4.ReadBoxInfo(reader)
		if err != nil {
			return PlaybackIndex{}, fmt.Errorf("read MP4 playback index: %w", err)
		}
		if box.Size < box.HeaderSize || box.Offset+box.Size > uint64(reader.size) {
			return PlaybackIndex{}, errors.New("MP4 playback box exceeds source length")
		}
		switch box.Type {
		case mp4.BoxTypeFtyp():
			hasFileType = true
		case mp4.BoxTypeMoov():
			hasMovie = true
			index.InitializationEnd = int64(box.Offset + box.Size - 1)
		case mp4.BoxTypeSidx():
			hasIndex = true
			index.IndexStart = int64(box.Offset)
			index.IndexEnd = int64(box.Offset + box.Size - 1)
		}
		if hasFileType && hasMovie && hasIndex {
			return index, nil
		}
		if _, err := box.SeekToEnd(reader); err != nil {
			return PlaybackIndex{}, err
		}
	}
	return PlaybackIndex{}, errors.New("MP4 source has no seekable segment index")
}

func probeWebMPlaybackIndex(reader *playbackRangeReader) (PlaybackIndex, error) {
	var prefix struct {
		EBML    webm.EBMLHeader
		Segment struct {
			SeekHead []webm.SeekHead
			Info     webm.Info
			Tracks   webm.Tracks
			Void     []byte
			CRC32    []byte
			Tags     struct{}
			Cluster  struct {
				Timecode uint64 `ebml:"Timecode,stop"`
			}
		}
	}
	segmentStart := int64(-1)
	var index PlaybackIndex
	stoppedAtCluster := false
	err := ebml.Unmarshal(reader, &prefix, ebml.WithElementReadHooks(func(element *ebml.Element) {
		if element.Parent != nil && element.Parent.Type == ebml.ElementSegment && (segmentStart < 0 || int64(element.Position) < segmentStart) {
			segmentStart = int64(element.Position)
		}
		if element.Type == ebml.ElementTimecode && element.Parent != nil && element.Parent.Type == ebml.ElementCluster {
			index.InitializationEnd = int64(element.Parent.Position) - 1
			stoppedAtCluster = true
		}
	}))
	if !errors.Is(err, ebml.ErrReadStopped) || !stoppedAtCluster || segmentStart < 0 {
		if err != nil {
			return PlaybackIndex{}, fmt.Errorf("read WebM initialization: %w", err)
		}
		return PlaybackIndex{}, errors.New("WebM source has no media initialization")
	}
	cuesAt := int64(-1)
	for _, head := range prefix.Segment.SeekHead {
		for _, seek := range head.Seek {
			if bytes.Equal(seek.SeekID, ebml.ElementCues.Bytes()) {
				cuesAt = segmentStart + int64(seek.SeekPosition)
				break
			}
		}
	}
	if cuesAt < 0 {
		return PlaybackIndex{}, errors.New("WebM source does not locate its seek index")
	}
	if _, err := reader.Seek(cuesAt, io.SeekStart); err != nil {
		return PlaybackIndex{}, err
	}
	var cues struct {
		Cues struct{} `ebml:"Cues,stop"`
	}
	err = ebml.Unmarshal(reader, &cues)
	if !errors.Is(err, ebml.ErrReadStopped) {
		if err != nil {
			return PlaybackIndex{}, fmt.Errorf("read WebM seek index: %w", err)
		}
		return PlaybackIndex{}, errors.New("WebM seek index is missing")
	}
	index.IndexStart, index.IndexEnd = cuesAt, reader.position-1
	return index, nil
}
