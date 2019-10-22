package nbdadapter

import (
	"context"
	"errors"
	"strconv"

	"github.com/abligh/gonbdserver/nbd"
)

type (
	SiaBackend struct {
		siaReaderWriter SiaReaderWriter
		size            uint64
	}

	SiaReaderWriter interface {
		ReadAt(b []byte, offset int64) (int, error)
		WriteAt(b []byte, offset int64) (int, error)
		Close() error
	}
)

func (sb *SiaBackend) WriteAt(ctx context.Context, b []byte, offset int64, fua bool) (int, error) {
	return sb.siaReaderWriter.WriteAt(b, offset)
}

func (sb *SiaBackend) ReadAt(ctx context.Context, b []byte, offset int64) (int, error) {
	return sb.siaReaderWriter.ReadAt(b, offset)
}

func (sb *SiaBackend) TrimAt(ctx context.Context, length int, offset int64) (int, error) {
	return length, nil
}

func (sb *SiaBackend) Flush(ctx context.Context) error {
	/* not implemented */
	return nil
}

func (sb *SiaBackend) Close(ctx context.Context) error {
	return sb.siaReaderWriter.Close()
}

func (sb *SiaBackend) Geometry(ctx context.Context) (uint64, uint64, uint64, uint64, error) {
	return sb.size, 1, 32 * 1024, 128 * 1024 * 1024, nil
}

func (sb *SiaBackend) HasFua(ctx context.Context) bool {
	return false
}

func (sb *SiaBackend) HasFlush(ctx context.Context) bool {
	return false
}

func NewSiaBackendFactory(getSiaReaderWriter func(size uint64) (SiaReaderWriter, error)) func(
	ctx context.Context, ec *nbd.ExportConfig) (nbd.Backend, error) {
	return func(ctx context.Context, ec *nbd.ExportConfig) (nbd.Backend, error) {
		sizeStr := ec.DriverParameters["size"]
		if sizeStr == "" {
			return nil, errors.New("no size given in configuration")
		}

		size, err := strconv.ParseUint(sizeStr, 10, 64)
		if err != nil {
			return nil, err
		}

		siaReaderWriter, err := getSiaReaderWriter(size)
		if err != nil {
			return nil, err
		}

		backend := SiaBackend{
			siaReaderWriter: siaReaderWriter,
			size:            size,
		}
		return &backend, nil
	}
}
