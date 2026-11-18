# 0c Windows x64 portable preview (P2-65A)

This ZIP is a portable engine bundle. It is not an installer and it is not a
complete input method: it contains the Go engine (`0c-server.exe`), the updater
tool (`0c-update.exe`), the pinned native ONNX Runtime DLLs, the exact
upstream v0.2.12 data pack, and the fetched Japanese model package. There is
no TSF DLL, no candidate window, no settings app and no Python runtime in the
archive.

## Requirements

- Windows 10 1803 or newer, or Windows 11, x64.
- No Python, no Visual C++ redistributable and no ONNX Runtime installation is
  required. All five runtime DLLs are next to `0c-server.exe`.
- PowerShell or Command Prompt.

The binaries are unsigned preview builds. Windows SmartScreen may warn. If the
ZIP came from a browser, right-click the ZIP, open Properties, select
Unblock, and extract it before running anything.

## Extract

Extract the whole archive to a normal folder, for example `C:\0c-preview`.
Keep this layout:

```text
0c-preview\
  0c-server.exe
  0c-update.exe
  onnxruntime.dll
  msvcp140.dll
  msvcp140_1.dll
  vcruntime140.dll
  vcruntime140_1.dll
  data\
  models\
  README.md
```

Do not move the DLLs away from `0c-server.exe` and do not move `data\` or
`models\` without passing the new paths explicitly.

## Check the data pack

Open PowerShell in the extracted folder:

```powershell
.\0c-server.exe -check-data -data .\data
```

Expected output: `資料包可載入` followed by exit code 0.

## Run the engine

```powershell
.\0c-server.exe -data .\data -user .\user.json -model-package-root .\models
```

The engine listens on the per-user named pipe `\\.\pipe\0c-<SID>`. It keeps
running until Ctrl+C. `user.json` is created in the same folder. Delete it to
reset personal learning.

For a short smoke run that exits by itself:

```powershell
.\0c-server.exe -data .\data -user .\user.json -idle-exit 30s
```

Because this bundle has no TSF shell and no test client, starting the engine
does not let you type into Windows applications. The engine and data pack can
be verified; actual typing needs a build that also ships `0c_tsf.dll`,
`0c-ui.exe` and the installer/registration flow.

`0c-update.exe` is included because the task requires it, but this ZIP is not
an installable update bundle: there is no `release.json`, no install receipt,
no TSF DLL and no `install.ps1`. Commands that need an installed layout will
fail with a missing-receipt error.

## What is in `data\`

`data\` is the exact upstream release data pack from
**0c-ime v0.2.12** (`assets/data/`). The source tarball
`0c-ime-0.2.12.tar.xz` was verified against the release `SHA256SUMS`
(tarball SHA-256 `7c9ba6877f25deb75a6068b849047bc5a2377eaddfc534529a2fdd1de2eff264`).
The tarball's `data/dictionary.lock.json` is byte-identical to
`data/dictionary.lock.json` in this repository, and all `assets/data/` files
match every size and SHA-256 in that lock. The dictionary bytes are the
upstream release bytes; they were not rebuilt in this worktree.

The data pack includes `lexicon.bin`, the full bigram-backed `lm.bin`
(52,766,129 bytes), manifest, NOTICE, report, and the `english/` subpack.

## What is in `models\`

The two int8 ONNX files were fetched with `0c-model-set` from the revisions
pinned in `models/pairing.lock.json`:

- `jeffpeng3/0c-ja-reranker` at `7450e10fe8b673560044c5db08ad3006a06d7a6e`
- `jeffpeng3/0c-ja-punct` at `ff9166abc620b6716ac7de73cfa67335e6b985d2`

Every SHA-256 matches the lock. The package is not activated: there is no
`active.json`, and the Japanese dictionary manifest pinned by the lock
(`4ba021b1...`) is not present in this source tree. The current engine's
Japanese conversion path also still reads TSV lexicon data and does not load
the ONNX reranker or punctuation model. The files are shipped for verification
and for the later integration step; they do not improve typing output in this
preview.

## Known limitations

1. VM acceptance is **NOT RUN**. This artifact was cross-compiled on Linux and
   has not been executed on Windows 10 or Windows 11.
2. No input method shell: no TSF DLL, no candidate window, no settings app.
   The engine alone cannot type into applications.
3. The data pack is the exact locked v0.2.12 release, but the engine is built
   from the current development tree, not from the v0.2.12 tag.
4. The Japanese model package is present but not active and not bound to a
   shipped Japanese data pack.
5. Binaries are unsigned. SmartScreen and antivirus products may warn.
6. No NSIS installer, no MSI/MSIX, no ARM64 build.
7. `0c-update.exe` cannot install or update this portable ZIP.
8. Only x64 processes are supported. This bundle has no 32-bit TSF DLL.

## Verify files

`SHA256SUMS` is not inside the ZIP. Use the SHA-256 published with this
artifact, or compute it on Windows:

```powershell
Get-FileHash .\0c-windows-x64-preview.zip -Algorithm SHA256
```
