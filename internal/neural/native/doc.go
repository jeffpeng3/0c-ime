// Package native is the opt-in Go ONNX Runtime backend.
//
// The real implementation is compiled only with `cgo && onnxruntime`; the
// default CGO-disabled build contains a stub whose Available reports false.
// Both variants expose the same API, so the residency manager always compiles
// and can fall back to the Python worker.
//
// The product default remains the Python worker until parity and packaging are
// complete. See platforms/neural/P2-30N-native-backend.md.
package native
