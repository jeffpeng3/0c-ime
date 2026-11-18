// Package native is the opt-in Go ONNX Runtime backend.
//
// The real implementation is compiled only with `cgo && onnxruntime`; the
// default CGO-disabled build contains a stub whose Available reports false.
// Both variants expose the same API, so the residency manager always compiles.
//
// Since P2-43 the Windows product builds with the tag, ships the pinned
// onnxruntime.dll and defaults to native; when the DLL is missing or a model
// shape is unsupported the manager degrades to statistical conversion without
// starting an interpreter. See models/runtime.lock.json.
package native
