"""Verify that ONNX Runtime is available and runs a model on this machine (P2-05).

The Windows workflow installs ``platforms/neural/requirements.txt`` and then runs
this script, so a broken wheel or a missing native DLL fails CI instead of the
first Phase 2 test that needs a real model. Models used by the product are not in
Git; the model here is built in memory and only checks the runtime.
"""

import sys

import numpy as np
import onnx
import onnxruntime as ort
from onnx import TensorProto, helper


def build_model():
    """Return a two-input Add model, the smallest graph every runtime supports."""
    node = helper.make_node("Add", ["a", "b"], ["c"])
    graph = helper.make_graph(
        [node],
        "ci_onnx_smoke",
        [helper.make_tensor_value_info("a", TensorProto.FLOAT, [2]),
         helper.make_tensor_value_info("b", TensorProto.FLOAT, [2])],
        [helper.make_tensor_value_info("c", TensorProto.FLOAT, [2])],
    )
    model = helper.make_model(
        graph, opset_imports=[helper.make_opsetid("", 17)])
    onnx.checker.check_model(model)
    return model.SerializeToString()


def main():
    providers = ort.get_available_providers()
    if "CPUExecutionProvider" not in providers:
        print(f"ONNX Runtime has no CPU execution provider: {providers}",
              file=sys.stderr)
        return 1
    session = ort.InferenceSession(build_model(),
                                   providers=["CPUExecutionProvider"])
    outputs = session.run(None, {
        "a": np.array([1.5, 2.5], dtype=np.float32),
        "b": np.array([2.0, 3.0], dtype=np.float32),
    })
    got = outputs[0].tolist()
    if got != [3.5, 5.5]:
        print(f"unexpected inference output: {got}", file=sys.stderr)
        return 1
    print(f"onnxruntime {ort.__version__} inference ok; providers {providers}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
