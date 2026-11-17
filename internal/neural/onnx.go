package neural

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// errONNXCorrupt：模型檔不是完整的 ONNX ModelProto。這裡只做結構探測，不驗證算子、
// 張量形狀或權重；真正的推論契約仍由 Python worker 載入時核對。目的在於把「截斷、
// 空檔、被換成別種檔案」歸類成 corrupt，而不是等到 worker 啟動才變成一般失敗。
var errONNXCorrupt = errors.New("onnx model is corrupt")

// validateONNXFile 串流讀取 protobuf 外框，要求 ir_version > 0、有 graph、有
// opset_import。ONNX 的欄位編號：1 ir_version、7 graph、8 opset_import。
func validateONNXFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() < 8 {
		return fmt.Errorf("%w: file is too small", errONNXCorrupt)
	}
	r := bufio.NewReaderSize(f, 64<<10)
	var hasVersion, hasGraph, hasOpset bool
	for {
		key, err := binary.ReadUvarint(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %v", errONNXCorrupt, err)
		}
		field, wire := key>>3, key&7
		if field == 0 {
			return fmt.Errorf("%w: field 0", errONNXCorrupt)
		}
		switch field {
		case 1: // ir_version
			if wire != 0 {
				return fmt.Errorf("%w: ir_version wire type %d", errONNXCorrupt, wire)
			}
			value, err := binary.ReadUvarint(r)
			if err != nil {
				return fmt.Errorf("%w: ir_version", errONNXCorrupt)
			}
			hasVersion = value > 0
		case 7: // graph
			if wire != 2 {
				return fmt.Errorf("%w: graph wire type %d", errONNXCorrupt, wire)
			}
			if err := skipLengthDelimited(r, true); err != nil {
				return err
			}
			hasGraph = true
		case 8: // opset_import
			if wire != 2 {
				return fmt.Errorf("%w: opset_import wire type %d", errONNXCorrupt, wire)
			}
			if err := skipLengthDelimited(r, true); err != nil {
				return err
			}
			hasOpset = true
		default:
			if err := skipField(r, wire); err != nil {
				return err
			}
		}
	}
	if !hasVersion || !hasGraph || !hasOpset {
		return fmt.Errorf("%w: missing ir_version, graph or opset_import", errONNXCorrupt)
	}
	return nil
}

func skipField(r *bufio.Reader, wire uint64) error {
	switch wire {
	case 0:
		if _, err := binary.ReadUvarint(r); err != nil {
			return fmt.Errorf("%w: varint", errONNXCorrupt)
		}
	case 1:
		if _, err := io.CopyN(io.Discard, r, 8); err != nil {
			return fmt.Errorf("%w: fixed64", errONNXCorrupt)
		}
	case 2:
		return skipLengthDelimited(r, false)
	case 5:
		if _, err := io.CopyN(io.Discard, r, 4); err != nil {
			return fmt.Errorf("%w: fixed32", errONNXCorrupt)
		}
	default:
		return fmt.Errorf("%w: unsupported wire type %d", errONNXCorrupt, wire)
	}
	return nil
}

func skipLengthDelimited(r *bufio.Reader, nonEmpty bool) error {
	n, err := binary.ReadUvarint(r)
	if err != nil || n > math.MaxInt64 {
		return fmt.Errorf("%w: length", errONNXCorrupt)
	}
	if nonEmpty && n == 0 {
		return fmt.Errorf("%w: empty message", errONNXCorrupt)
	}
	if _, err := io.CopyN(io.Discard, r, int64(n)); err != nil {
		return fmt.Errorf("%w: truncated message", errONNXCorrupt)
	}
	return nil
}
