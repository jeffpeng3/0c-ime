# Placeholder model-set fixture

`pairing.lock.json` is the P2-25P four-model placeholder manifest, kept only as a
test fixture. It is not the product lock and must never enter a public release
bundle.

The product lock is `models/pairing.lock.json`; its artifacts point at the four
Hugging Face repositories and have `placeholder: false`. Tests that need the old
placeholder metadata (hash checks, `-allow-placeholders`, preview packaging) read
this file explicitly.
