package native

// Minimal ONNX ModelProto reader for graph input/output metadata.
//
// onnxruntime_go can only report the graph signature by creating a session,
// which loads the whole model; doing that and then creating the real session
// parses a 130 MB model twice. The sequence backend needs the signature before
// it can create a session, so it reads the protobuf instead. Only the fields
// needed for layout detection are decoded; everything else is skipped without
// buffering the graph.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

type onnxValue struct {
	Name     string
	ElemType int32
	Dims     []int64 // 0 for a symbolic/dynamic dimension
}

// ONNX ModelProto / GraphProto / ValueInfoProto field numbers.
const (
	modelGraph     = 7
	graphInit      = 5
	graphInput     = 11
	graphOutput    = 12
	tensorName     = 1
	valueInfoName  = 1
	valueInfoType  = 2
	typeTensor     = 1
	tensorElemType = 1
	tensorShape    = 2
	shapeDim       = 1
	dimValue       = 1
)

var errONNXIO = errors.New("onnx graph signature is unreadable")

func readONNXIO(path string) ([]onnxValue, []onnxValue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	pos := int64(0)
	p := protoReader{r: bufio.NewReaderSize(f, 64<<10), pos: &pos, end: math.MaxInt64}
	inputs, outputs, found := []onnxValue{}, []onnxValue{}, false
	for {
		field, wire, err := p.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", errONNXIO, err)
		}
		if field == modelGraph && wire == 2 {
			sub, err := p.sub()
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %v", errONNXIO, err)
			}
			inputs, outputs, err = readGraph(sub)
			if err != nil {
				return nil, nil, err
			}
			found = true
			continue
		}
		if err := p.skip(wire); err != nil {
			return nil, nil, fmt.Errorf("%w: %v", errONNXIO, err)
		}
	}
	if !found {
		return nil, nil, fmt.Errorf("%w: no graph", errONNXIO)
	}
	return inputs, outputs, nil
}

func readGraph(p protoReader) ([]onnxValue, []onnxValue, error) {
	initializers := map[string]bool{}
	var rawInputs, outputs []onnxValue
	for {
		field, wire, err := p.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", errONNXIO, err)
		}
		switch {
		case field == graphInit && wire == 2:
			sub, err := p.sub()
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %v", errONNXIO, err)
			}
			name, err := readTensorName(sub)
			if err != nil {
				return nil, nil, err
			}
			if err := sub.discard(sub.end - *sub.pos); err != nil {
				return nil, nil, fmt.Errorf("%w: %v", errONNXIO, err)
			}
			if name != "" {
				initializers[name] = true
			}
		case (field == graphInput || field == graphOutput) && wire == 2:
			sub, err := p.sub()
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %v", errONNXIO, err)
			}
			v, err := readValueInfo(sub)
			if err != nil {
				return nil, nil, err
			}
			if field == graphInput {
				rawInputs = append(rawInputs, v)
			} else {
				outputs = append(outputs, v)
			}
		default:
			if err := p.skip(wire); err != nil {
				return nil, nil, fmt.Errorf("%w: %v", errONNXIO, err)
			}
		}
	}
	// IR < 4 lists initializers in graph.input; they are not session inputs.
	inputs := rawInputs[:0]
	for _, v := range rawInputs {
		if !initializers[v.Name] {
			inputs = append(inputs, v)
		}
	}
	return inputs, outputs, nil
}

func readTensorName(p protoReader) (string, error) {
	for {
		field, wire, err := p.next()
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("%w: %v", errONNXIO, err)
		}
		if field == tensorName && wire == 2 {
			return p.string()
		}
		if err := p.skip(wire); err != nil {
			return "", fmt.Errorf("%w: %v", errONNXIO, err)
		}
	}
}

func readValueInfo(p protoReader) (onnxValue, error) {
	var v onnxValue
	for {
		field, wire, err := p.next()
		if errors.Is(err, io.EOF) {
			return v, nil
		}
		if err != nil {
			return v, fmt.Errorf("%w: %v", errONNXIO, err)
		}
		switch {
		case field == valueInfoName && wire == 2:
			v.Name, err = p.string()
			if err != nil {
				return v, fmt.Errorf("%w: %v", errONNXIO, err)
			}
		case field == valueInfoType && wire == 2:
			sub, err := p.sub()
			if err != nil {
				return v, fmt.Errorf("%w: %v", errONNXIO, err)
			}
			if err := readType(sub, &v); err != nil {
				return v, err
			}
		default:
			if err := p.skip(wire); err != nil {
				return v, fmt.Errorf("%w: %v", errONNXIO, err)
			}
		}
	}
}

func readType(p protoReader, v *onnxValue) error {
	for {
		field, wire, err := p.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", errONNXIO, err)
		}
		if field == typeTensor && wire == 2 {
			sub, err := p.sub()
			if err != nil {
				return fmt.Errorf("%w: %v", errONNXIO, err)
			}
			if err := readTensorType(sub, v); err != nil {
				return err
			}
			continue
		}
		if err := p.skip(wire); err != nil {
			return fmt.Errorf("%w: %v", errONNXIO, err)
		}
	}
}

func readTensorType(p protoReader, v *onnxValue) error {
	for {
		field, wire, err := p.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", errONNXIO, err)
		}
		switch {
		case field == tensorElemType && wire == 0:
			n, err := p.varint()
			if err != nil {
				return fmt.Errorf("%w: %v", errONNXIO, err)
			}
			v.ElemType = int32(n)
		case field == tensorShape && wire == 2:
			sub, err := p.sub()
			if err != nil {
				return fmt.Errorf("%w: %v", errONNXIO, err)
			}
			dims, err := readShape(sub)
			if err != nil {
				return err
			}
			v.Dims = dims
		default:
			if err := p.skip(wire); err != nil {
				return fmt.Errorf("%w: %v", errONNXIO, err)
			}
		}
	}
}

func readShape(p protoReader) ([]int64, error) {
	var dims []int64
	for {
		field, wire, err := p.next()
		if errors.Is(err, io.EOF) {
			return dims, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errONNXIO, err)
		}
		if field == shapeDim && wire == 2 {
			sub, err := p.sub()
			if err != nil {
				return nil, fmt.Errorf("%w: %v", errONNXIO, err)
			}
			d, err := readDim(sub)
			if err != nil {
				return nil, err
			}
			dims = append(dims, d)
			continue
		}
		if err := p.skip(wire); err != nil {
			return nil, fmt.Errorf("%w: %v", errONNXIO, err)
		}
	}
}

func readDim(p protoReader) (int64, error) {
	var size int64
	for {
		field, wire, err := p.next()
		if errors.Is(err, io.EOF) {
			return size, nil
		}
		if err != nil {
			return 0, fmt.Errorf("%w: %v", errONNXIO, err)
		}
		if field == dimValue && wire == 0 {
			n, err := p.varint()
			if err != nil {
				return 0, fmt.Errorf("%w: %v", errONNXIO, err)
			}
			size = int64(n)
			continue
		}
		if err := p.skip(wire); err != nil {
			return 0, fmt.Errorf("%w: %v", errONNXIO, err)
		}
	}
}

// protoReader reads one protobuf message from a shared buffered stream. pos is
// shared with sub-messages so parent and child limits stay consistent.
type protoReader struct {
	r   *bufio.Reader
	pos *int64
	end int64
}

func (p protoReader) next() (field int, wire int, err error) {
	key, err := p.varint()
	if err != nil {
		return 0, 0, err
	}
	field, wire = int(key>>3), int(key&7)
	if field == 0 {
		return 0, 0, errors.New("field 0")
	}
	if wire != 0 && wire != 1 && wire != 2 && wire != 5 {
		return 0, 0, fmt.Errorf("unsupported wire type %d", wire)
	}
	return field, wire, nil
}

func (p protoReader) varint() (uint64, error) {
	var v uint64
	for shift := 0; shift < 64; shift += 7 {
		if *p.pos >= p.end {
			return 0, io.EOF
		}
		b, err := p.r.ReadByte()
		if err != nil {
			return 0, err
		}
		*p.pos++
		v |= uint64(b&0x7f) << shift
		if b < 0x80 {
			return v, nil
		}
	}
	return 0, errors.New("varint overflow")
}

func (p protoReader) sub() (protoReader, error) {
	n, err := p.varint()
	if err != nil {
		return protoReader{}, err
	}
	if n > uint64(p.end-*p.pos) {
		return protoReader{}, errors.New("message length past end")
	}
	return protoReader{r: p.r, pos: p.pos, end: *p.pos + int64(n)}, nil
}

func (p protoReader) string() (string, error) {
	sub, err := p.sub()
	if err != nil {
		return "", err
	}
	b := make([]byte, sub.end-*sub.pos)
	if _, err := io.ReadFull(sub.r, b); err != nil {
		return "", err
	}
	*sub.pos += int64(len(b))
	return string(b), nil
}

func (p protoReader) skip(wire int) error {
	switch wire {
	case 0:
		_, err := p.varint()
		return err
	case 1:
		return p.discard(8)
	case 2:
		sub, err := p.sub()
		if err != nil {
			return err
		}
		return sub.discard(sub.end - *sub.pos)
	case 5:
		return p.discard(4)
	}
	return errors.New("unsupported wire type")
}

func (p protoReader) discard(n int64) error {
	if n < 0 || n > p.end-*p.pos {
		return io.ErrUnexpectedEOF
	}
	if _, err := io.CopyN(io.Discard, p.r, n); err != nil {
		return err
	}
	*p.pos += n
	return nil
}
