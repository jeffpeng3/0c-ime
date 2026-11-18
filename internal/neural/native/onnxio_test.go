package native

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Minimal protobuf writer for the synthetic graph-signature fixture.
func pbVarint(v uint64) []byte {
	var b []byte
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func pbKey(field, wire int) []byte { return pbVarint(uint64(field<<3 | wire)) }

func pbBytes(field int, b []byte) []byte {
	out := pbKey(field, 2)
	out = append(out, pbVarint(uint64(len(b)))...)
	return append(out, b...)
}

func pbString(field int, s string) []byte { return pbBytes(field, []byte(s)) }

func pbUint(field int, v uint64) []byte {
	return append(pbKey(field, 0), pbVarint(v)...)
}

func valueInfo(name string, elem int, dims ...int64) []byte {
	var shape []byte
	for _, d := range dims {
		var dim []byte
		if d > 0 {
			dim = pbUint(dimValue, uint64(d))
		} else {
			dim = pbString(2, "dynamic")
		}
		shape = append(shape, pbBytes(shapeDim, dim)...)
	}
	tensor := append(pbUint(tensorElemType, uint64(elem)), pbBytes(tensorShape, shape)...)
	typ := pbBytes(typeTensor, tensor)
	return append(pbString(valueInfoName, name), pbBytes(valueInfoType, typ)...)
}

func TestReadONNXIO(t *testing.T) {
	graph := []byte{}
	graph = append(graph, pbBytes(graphInput, valueInfo("ids", 7, 1, 0))...)
	graph = append(graph, pbBytes(graphInput, valueInfo("mask", 9, 1, 1, 0, 0))...)
	graph = append(graph, pbBytes(graphInput, valueInfo("weight", 7, 1))...)
	init := append(pbString(tensorName, "weight"), pbBytes(9, bytes.Repeat([]byte{0x01}, 16))...)
	graph = append(graph, pbBytes(graphInit, init)...)
	graph = append(graph, pbBytes(graphOutput, valueInfo("scores", 1, 0))...)
	// An unrelated field the reader must skip.
	graph = append(graph, pbString(2, "producer")...)
	model := append(pbUint(1, 8), pbBytes(modelGraph, graph)...)
	path := filepath.Join(t.TempDir(), "model.onnx")
	if err := os.WriteFile(path, model, 0o644); err != nil {
		t.Fatal(err)
	}
	inputs, outputs, err := readONNXIO(path)
	if err != nil {
		t.Fatal(err)
	}
	wantInputs := []onnxValue{
		{Name: "ids", ElemType: 7, Dims: []int64{1, 0}},
		{Name: "mask", ElemType: 9, Dims: []int64{1, 1, 0, 0}},
	}
	if !reflect.DeepEqual(inputs, wantInputs) {
		t.Fatalf("inputs = %+v, want %+v", inputs, wantInputs)
	}
	wantOutputs := []onnxValue{{Name: "scores", ElemType: 1, Dims: []int64{0}}}
	if !reflect.DeepEqual(outputs, wantOutputs) {
		t.Fatalf("outputs = %+v, want %+v", outputs, wantOutputs)
	}
}

func TestReadONNXIORejectsTruncated(t *testing.T) {
	graph := pbBytes(graphInput, valueInfo("ids", 7, 1))
	model := pbBytes(modelGraph, graph)
	path := filepath.Join(t.TempDir(), "model.onnx")
	if err := os.WriteFile(path, model[:len(model)-3], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readONNXIO(path); err == nil {
		t.Fatal("truncated model accepted")
	}
}

func TestReadONNXIORejectsNoGraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.onnx")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0x08, 0x01}, 4), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readONNXIO(path); err == nil {
		t.Fatal("model without graph accepted")
	}
}
