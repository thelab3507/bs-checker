# Attempting to call BS on open weight decision models

I wanted to call BS on models that say "92% sure". Maybe it's really 50% and they're just trained to sound confident. So I built this to check.

When a decision model says "92% sure", is it right 92% of the time? BS Meter is a command-line tool that checks. It asks a model a few hundred yes/no questions with known answers, then compares how confident each answer was with how often it was right, and writes one self-contained HTML report with charts, error bars and a plain verdict: overconfident, underconfident or honest.

## Why this exists

Decision models answer a yes/no question or pick an option and attach a confidence number, and people are starting to build automation on it: "if over 90% sure, approve; otherwise ask a human". A [Hightouch post](https://hightouch.com/blog/llm-judges-identity-resolution) credited the edge of TypeSafe's Jev, a closed, paid decision model, to its calibrated confidence. I didn't test Jev because it costs money, so I tested the open decision models you can run yourself. They mostly get used for simple delegated tasks, so I also wanted a first read on whether they could handle heavier ones.

## The first results

Three small models, all Q4_K_M, on BoolQ, an SMS spam set and a small fresh-news set: Liquid d1-2B and Kev-4B (decision models) and Gemma 4 E2B (an ordinary chat model forced to say Yes or No, as a control).

My suspicion held up for Gemma, not for the decision models.

1. **The decision models do not overstate their confidence.** d1-2B is 5 to 6 points underconfident on BoolQ and SMS spam. Kev-4B is 2 points under on BoolQ (no miscalibration detected) and 13 points under on SMS spam, where none of its answers reached 95% confidence.
2. **The control is badly overconfident.** Gemma claims 95% or more on 533 of 600 BoolQ answers and is right on 75% of them.
3. **No ranking.** d1 and Kev are better calibrated on different sets, and Gemma gets its confidence a different way. fresh-news has too few independent stories (18) to judge.

Read the full writeup, with the numbers, the limits and what went wrong: [`docs/writeup.md`](docs/writeup.md). Download and open [`results/report-mac-metal.html`](results/report-mac-metal.html) to see the report (GitHub shows HTML files as source), and [`results/README.md`](results/README.md) to see how it was produced and how to check it.

## Quick start

You need [Go](https://go.dev/dl/) 1.27 or newer. Nothing else is required to try the tool on the bundled example data:

```
go run ./cmd/bsmeter analyze -results examples/results.example.jsonl -out analysis.json
go run ./cmd/bsmeter report -analysis analysis.json -out report.html
```

Open `report.html` in a browser. It is one file with its charts and font inside, no scripts and no network use. A sample made from synthetic data is in [`examples/report.example.html`](examples/report.example.html).

## Running a model

You need [`llama.cpp`](https://github.com/ggml-org/llama.cpp)'s `llama-server` on your PATH (or its full path in `models.json`), with `/v1/systemone` support if you want to test a decision model such as d1 or Kev. The models in `models.json` download through `llama-server -hf` the first time.

```
go run ./cmd/bsmeter run -host my-machine -out results.jsonl
go run ./cmd/bsmeter analyze -results results.jsonl -out analysis.json
go run ./cmd/bsmeter report -analysis analysis.json -out report.html -csv-dir csv
```

- `-host` is a label for your machine and backend, such as `mac-metal` or `win-cuda`. Results from different hosts are kept apart and never compared.
- `run` starts one `llama-server` per model, one at a time, and appends every answer to the results file as it arrives. If it crashes or your computer sleeps, run the same command again and it resumes where it stopped.
- `analyze -quant Q4_K_M` analyses only rows of that quantisation (repeatable). The default is every quantisation in the file.
- `-limit 20` asks only the first 20 questions of each set, which is a quick way to check your setup. `-only kev-4b:Q4_K_M` runs one model.
- The mean time per answer on the Mac the published run came from was 37 to 39 milliseconds for d1 and Gemma and 91 for Kev, so the full run of the bundled models and sets (3,903 answers) takes a few minutes.

### Testing your own model

`models.json` lists what to run. Add an entry for each model:

| Field | Meaning |
| --- | --- |
| `label`, `quant` | Names written to every result row. `quant` records which model file was used, such as `Q4_K_M`. The pair must be unique. |
| `method` | `systemone` for decision models that `llama-server` serves through `/v1/systemone`; `tokenprob` for an ordinary chat model, which is asked for a one-word Yes or No and judged on the probabilities of those two words. |
| `hf` | A Hugging Face `repo:quant`, passed to `llama-server -hf`. |
| `port` | Port for the server the tool launches, 1024 to 65535. It must be free. |
| `extra_args` | Optional extra `llama-server` flags. |

`examples/models.example.json` is a complete file. Keep the default launch flags unless you know you need to change them: d1-2B fails on prompts over 512 tokens without a larger batch size, and the defaults already set it. Kev-4B needs nothing extra.

### Testing your own questions

Questions are JSONL, one per line, and `-questions my-set.jsonl` runs them (the flag can be repeated):

| Field | Meaning |
| --- | --- |
| `id` | Unique within the set. |
| `set` | The set's name. |
| `passage` | Optional context shown to the model. |
| `question` | The yes/no question. |
| `options` | Exactly two, for example `["Yes","No"]`. The first counts as the positive one. |
| `answer` | The correct option. |
| `source` | Where the question came from. Required by the checker. |

`examples/questions.example.jsonl` is a worked example, and `go run ./cmd/mkdata validate my-set.jsonl` checks a file. If several questions share a passage, give them ids of the form `name-<story>-<number>` and pass `-cluster 'name=^name-(.+)-\d+$'` to `analyze`, so that questions about one story are resampled together.

### Files the tool writes

- **Results file:** one JSON line per answer: model, quantisation, host, set, question id, the option chosen, its probability (`p_chosen`, the confidence), whether it was correct, and the latency. Examples are in `examples/`.
- **`analysis.json`:** every number, each with a 95% interval: accuracy, calibration error, signed gap, Brier score, log loss, reliability bands, the danger zone, and the correction.
- **The report and CSVs:** the same numbers as a page, and as `runs.csv` for a spreadsheet.

## What it measures

- **Accuracy**, **expected calibration error** (ECE: the average gap between claimed confidence and how often the model was right), the **signed gap** (average confidence minus accuracy: positive means overconfident), **Brier score** and **log loss**.
- **A reliability chart:** answers grouped by confidence, claim against outcome.
- **The danger zone:** of the answers at 95% or more confidence, the share that were right. That is the case an auto-approve rule trusts.
- **A 95% interval on everything**, by resampling.
- **A simple correction:** temperature scaling, fitted on a random half of each set and tested on the other half, saved as a small JSON file other tools can apply.

### How verdicts are decided

The rules were written down before the first full run.

- **Each run** is judged from the interval of the signed gap. Wholly above zero is *overconfident*, wholly below zero is *underconfident*, spanning zero is *honest*. A run with fewer than 100 answers, or fewer than 30 independent groups of questions, is *too few answers to judge*, whatever its gap.

## Known limitations

- Yes/no questions only. Two options per question.
- The published result covers three small models (one Q4_K_M file each), one Mac and one llama.cpp build. See the limits in the [writeup](docs/writeup.md).
- Different confidence mechanisms (the native `/v1/systemone` endpoint of d1 and Kev, and Gemma's token probabilities) are not comparable with each other. d1 and Kev share one, so they can be compared with each other, but two models and three sets do not make a ranking.
- I haven't tested Jev.
- So far it's only the lightest kind of task: yes/no questions about short texts.
- It measures whether the number can be trusted, not whether the answer is right.
- The tool has been run end to end on macOS (Metal). On Windows with an NVIDIA GPU it ran a partial check. The test suite runs on Linux, macOS and Windows in CI.

## Future work

- Jev, if a cheap one-off run is worth paying for.
- Clef-Flash, a marginal fit for an 8 GB graphics card (6.73 GB at Q4_K_M).
- A rephrasing test and an "I don't know" test, which should need new question files and no new code.

## Data and licences

- **Code:** MIT, see [`LICENSE`](LICENSE).
- **`data/boolq.jsonl`:** from BoolQ (Clark et al., NAACL 2019), licensed CC BY-SA 3.0. That file keeps its own licence, not the repo's.
- **`data/sms-spam.jsonl`:** from the UCI SMS Spam Collection (Almeida et al., DocEng 2011), CC BY 4.0.
- **`data/fresh-news.jsonl`:** written for this project from web searches, with sources listed per question. It is for testing reading, not for quoting as news.
- **Font:** Plus Jakarta Sans, SIL Open Font License, embedded in the report. The licence text is in `internal/report/assets/`.

Details and citations are in [`data/README.md`](data/README.md).

## How this was made

I came up with the question, picked the models and test sets, and set the verdict rules before the full run. An AI coding assistant (Anthropic's Claude Code) wrote most of the code, the tests and the fresh-news questions, and I reviewed the results and the docs. The labels were re-read against their passages, and the analysis and report can be regenerated from the raw answers (see [`results/README.md`](results/README.md)).

## Repository layout

| Path | What is in it |
| --- | --- |
| `cmd/bsmeter` | The tool: `run`, `analyze`, `report` |
| `cmd/mkdata` | Builds and checks question sets |
| `internal/` | Shared code: asking, calibration maths, analysis, report |
| `data/` | The three question sets and where each came from |
| `results/` | The published run: raw answers, analysis, report, CSVs |
| `examples/` | Small worked examples of every file |
| `docs/` | The writeup |
