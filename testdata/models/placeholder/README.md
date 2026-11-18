# Placeholder model package fixture

`pairing.lock.json` is the P2-25P placeholder manifest (one model package: a
CC BY-SA placeholder reranker plus an MIT placeholder punctuation model), kept
only as a test fixture. It is not the product lock and must never enter a
public release bundle.

The product lock is `models/pairing.lock.json`; its two artifacts point at
`jeffpeng3/0c-ja-reranker` and `jeffpeng3/0c-ja-punct`, are pinned to a
Hugging Face revision, and have `placeholder: false`. Tests that need placeholder
metadata (hash checks, `-allow-placeholders`, preview packaging) read this file
explicitly.
