# `0c-jaeval` mixed conversion regression harness

`cmd/0c-jaeval` runs a committed corpus of mixed input strings against the
single Mixed profile: Chinese zhuyin, Japanese romaji and English in one
harness. It is deterministic, needs no network, and never loads an ONNX model.

The command is meant for human-readable diffs and CI snapshot checks, not for
choosing a release quality threshold. Its corpus is small and hand-written on
purpose; the snapshot tells you that a conversion changed, not whether the new
conversion is linguistically better.

## Quick start

With the repository's real data:

```sh
go run ./cmd/0c-jaeval -report
```

The defaults are:

| flag | default | meaning |
| --- | --- | --- |
| `-corpus` | `testdata/jaeval/corpus.json` | committed mixed-input corpus |
| `-snapshot` | `testdata/jaeval/snapshot.json` | expected per-case snapshot |
| `-data` | `data/ja` | Japanese `lexicon/words.tsv` and `connection.tsv` |
| `-zh-data` | `testdata/golden/zh/data` | Chinese `lexicon.tsv` and `lm.tsv` |
| `-candidates` | `5` | maximum candidates kept per case |
| `-update` | off | rewrite the snapshot from current output |
| `-report` | off | print aggregate statistics after the table |

By default the command prints one row per case (input, conversion, expected
surface, expected rank and candidate count), compares the output with the
snapshot, and exits non-zero on any changed case.

### Statistical fallback

The harness loads whatever data is present. If a Japanese lexicon or Chinese
lexicon/LM is missing, that language falls back instead of failing:

* Japanese romaji still composes to kana, but kana-kanji conversion is skipped
  and the chosen candidate is the kana reading.
* Zhuyin input stays literal when the Chinese lexicon/LM is absent.
* English, numbers, symbols and whitespace always stay literal.

To exercise the fallback explicitly:

```sh
go run ./cmd/0c-jaeval -data= -zh-data= -report
```

Fallback output will not match the committed snapshot, which was generated with
the repository's statistical data. That is expected: the snapshot freezes the
real-data run.

## Snapshot semantics

`testdata/jaeval/snapshot.json` stores, in corpus order:

* the corpus name and corpus file SHA-256;
* for every case: id, category, input, expected surface, chosen output,
  candidate list and the rank of the expected surface (`0` means absent);
* a `sha256` checksum of the canonical payload.

The checksum is SHA-256 over the canonical compact JSON of the payload with the
`sha256` field removed. There are no timestamps, host names or map iteration
orders in that payload. Changing only file indentation does not change it.
Changing a corpus case changes `corpus_sha256` and therefore the snapshot
checksum even if the output happens to stay the same.

When a run differs, the command prints only the changed cases and fields with
snapshot/current values, for example:

```text
snapshot mismatch: 1 change(s)
case ja-word-kyou: output
  - 今日
  + きょう
```

After confirming the change is intentional:

```sh
go run ./cmd/0c-jaeval -update
```

`-update` writes the snapshot, prints its new SHA-256, and never modifies the
corpus.

## Corpus format

`testdata/jaeval/corpus.json` has a stable `schema`, a `count`, and cases with:

```json
{
  "id": "mix-jin-tian-kyou",
  "category": "mixed-zh-ja",
  "input": "ㄐㄧㄣㄊㄧㄢkyou",
  "expect": "今天今日",
  "note": "optional"
}
```

Whitespace in `input` is preserved literally. That makes English and punctuation
easy to place: `kyou API desu` produces `今日 API です`.

Zhuyin is written as concatenated Bopomofo without spaces. The harness splits
syllables by zhuyin class order (initial, medial, final, tone), so
`ㄐㄧㄣㄊㄧㄢ` becomes two syllables. A space is also accepted between zhuyin
syllables, but it is preserved in the output.

The committed corpus covers these categories:

| category | coverage |
| --- | --- |
| `ja-word` | pure Japanese romaji nouns and adverbs |
| `ja-okurigana` | verbs and adjectives with okurigana |
| `ja-particle` | particles attached to words |
| `ja-sentence` | long particle sentences |
| `katakana` | katakana loanwords (romaji/kana input and direct katakana) |
| `mixed-zh-ja` | zhuyin and Japanese romaji in the same string |
| `ja-english` | English words and identifiers inside Japanese input |
| `numbers-symbols` | digits, signs, percent and mixed identifiers |
| `punctuation` | punctuation at commit, including full-width Japanese/Chinese punctuation |

## Report

`-report` prints aggregate statistics:

* number and rate of cases whose expected surface is ranked first;
* average number of candidates per case;
* the longest conversion and its case id;
* the same top-1 and candidate numbers per category, sorted by category name.

The corpus intentionally includes one known katakana limitation
(`めーる` → `目ーる` with the current `PreferHiragana` mixed profile), so the
baseline is not a claim of 100% accuracy. It is a regression baseline.

## Tests

The package tests use a three-case fixture, not the full dictionaries:

```sh
go test ./internal/jaeval
```

They cover corpus parsing/validation, snapshot match/mismatch/update and
checksum rejection, report aggregation, zhuyin splitting, and the no-data
fallback.

## Determinism checklist

* corpus and snapshot use file order, never map order;
* report categories are sorted by name, snapshot diffs are sorted by case id
  and field;
* candidate combination has a fixed first-group-slowest order;
* no clocks, randomness, network, personal learning or ONNX models;
* the hashed snapshot payload excludes the checksum field and all timestamps.
