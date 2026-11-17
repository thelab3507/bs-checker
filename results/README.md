# Results: Mac (Metal), 13 and 16 November 2026

These are the published numbers behind the writeup in [`docs/writeup.md`](../docs/writeup.md). Everything here is derived from one file of raw answers, and the tests in `cmd/bsmeter/published_test.go` fail if the files stop agreeing.

| File | What it is |
| --- | --- |
| `results-mac-metal.jsonl` | The raw answers: one line per question per run, 6,505 lines, including 2,602 Q8_0 rows from an earlier 4-bit vs 8-bit comparison. Those rows are kept but not analysed. Nothing else in this folder is a measurement; the rest is computed from this file. |
| `analysis-mac-metal.json` | Every number: accuracy, calibration error (ECE), signed gap, Brier score, log loss, reliability bands, the danger zone, and the held-out correction, each with a 95% interval. Q4_K_M runs only. |
| `report-mac-metal.html` | The same analysis as one self-contained page. Open it in any browser. |
| `csv-mac-metal/` | `runs.csv` (one row per run), for a spreadsheet. |
| `corrections-mac-metal/` | The fitted temperature-scaling correction for each run that had enough data. |

## How it was produced

- **Machine:** a MacBook Pro running macOS. Host label `mac-metal`.
- **Server:** `llama-server` build 12110 (Homebrew), Metal backend.
- **When:** d1 and Gemma on 13 November 2026 (33 to 39 ms per answer). Kev-4B was added on 16 November 2026 on the same Mac and the same `llama-server` build, with the same host label (91 ms per answer).
- **Questions:** the three files in [`data/`](../data/README.md): BoolQ (600), SMS Spam (500) and fresh-news (201).
- **Models**, all downloaded from Hugging Face through `llama-server -hf`:

| Label | Quant | Source | File size | SHA-256 |
| --- | --- | --- | --- | --- |
| lfm2-d1-2b | Q4_K_M | `LiquidAI/d1-2B-GGUF` | 1.10 GB | `adca5789ad30ae6027e8e936749bb9f19673097492ab26fa1c5b69643ca50b4b` |
| lfm2-d1-2b | Q8_0 | `LiquidAI/d1-2B-GGUF` | 1.85 GB | `073313d92b53b0ac8193db6e906cb3c78a98183d244b15b7b8f5426115c6738c` |
| gemma-4-e2b-it | Q4_K_M | `unsloth/gemma-4-E2B-it-GGUF` | 3.11 GB | `740185b21d22ceb83a11c3aa62ad5842ef32c70f6096d756bbee85a1e4ec34b8` |
| gemma-4-e2b-it | Q8_0 | `unsloth/gemma-4-E2B-it-GGUF` | 5.05 GB | `605d3c2647d7c58c1e4b5375ccb5702acf94c2611b4c8d4877812f8fdd32d053` |
| kev-4b | Q4_K_M | `ggml-org/Kev-4B-GGUF` | 3.03 GB | `33ae6b18926502b2209a1bf7d3b61a350d65938441515c686bb87d226a19eff9` |

The two Q8_0 files produced the kept-but-not-analysed rows; every analysed run is Q4_K_M.

- **Confidence:** d1 and Kev through the native `/v1/systemone` endpoint (`systemone`); Gemma by reading the probabilities of the Yes and No tokens (`tokenprob`). d1 and Kev use the same mechanism, so they can be compared with each other. Neither is directly comparable with Gemma, which uses a different one.
- **Analysis settings:** seed 1, 1,000 bootstrap resamples, danger zone at 95% confidence, whole stories resampled for fresh-news. These are the defaults of `bsmeter analyze`.

Rows from different machines are never compared by the tool. This folder holds a single host.

## Reproduce it

From the repository root, with Go installed:

```
go run ./cmd/bsmeter analyze -results results/results-mac-metal.jsonl -quant Q4_K_M -out /tmp/analysis.json
go run ./cmd/bsmeter report -analysis results/analysis-mac-metal.json -out /tmp/report.html -title "BS Meter on mac-metal"
cmp /tmp/report.html results/report-mac-metal.html
```

The analysis matches `analysis-mac-metal.json` to within floating-point rounding. The last digit can differ between an Apple chip and an Intel or AMD one, which is why the test compares numbers with a tolerance and not byte for byte. The report is a pure function of the analysis file, so `cmp` prints nothing.

To redo the measurement itself you need `llama-server` and the three Q4_K_M model files listed in `models.json`. Run `go run ./cmd/bsmeter run -host <your-host-label> -out results/your-results.jsonl`. Answers from a different machine or llama.cpp build will differ slightly, and that is expected.
