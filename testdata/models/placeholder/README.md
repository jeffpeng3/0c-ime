# Placeholder model-set fixture

`pairing.lock.json` is the P2-25P placeholder manifest (three artifacts: the
public-core reranker and punctuation plus the reranker-only experimental
reranker), kept only as a test fixture. It is not the product lock and must never enter a
public release bundle.

The product lock is `models/pairing.lock.json`; its artifacts point at the three
Hugging Face repositories and have `placeholder: false`. Tests that need placeholder metadata (hash checks, `-allow-placeholders`, preview
packaging) read this file explicitly.
