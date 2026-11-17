# Question sets

Three yes/no sets in the question-set format described in the main [README](../README.md). Every question has a passage, and the answer is whatever the passage supports. In each file `options` is `["Yes","No"]`.

| File | Questions | Yes share | Source |
| --- | --- | --- | --- |
| `boolq.jsonl` | 600 | 61% | BoolQ validation split |
| `sms-spam.jsonl` | 500 | 50% | UCI SMS Spam Collection |
| `fresh-news.jsonl` | 201 | 60% | Written by an AI assistant from news searches, Oct–Nov 2026 |

## Rebuilding

```sh
go run ./cmd/mkdata boolq      # data/boolq.jsonl, 600 questions, seed 1
go run ./cmd/mkdata sms        # data/sms-spam.jsonl, 500 messages, seed 1
go run ./cmd/mkdata validate data/*.jsonl
go run ./cmd/mkdata sample-ids -pct 10 data/fresh-news.jsonl
```

A rerun of `boolq` and `sms` produces byte-identical files (checked). Sampling uses Go's `math/rand/v2` PCG generator, whose output is guaranteed stable. Each source's row count is checked (3,270 and 5,574), so a changed upstream fails loudly instead of silently shifting the sample.

## BoolQ

- Source: `google/boolq`, validation split, read through the Hugging Face rows API (the original Google download returns 403).
- Licence: CC BY-SA 3.0. `boolq.jsonl` carries the same licence, not the repo's MIT. Citation: Clark et al., "BoolQ: Exploring the Surprising Difficulty of Natural Yes/No Questions", NAACL 2019.
- Sampling: 600 of 3,270 at random, seed 1. IDs are `boolq-<row index>`, so a question can be traced back to its row.
- The Hugging Face copy has no `title` field, so the passage is the paragraph alone. BoolQ questions arrive lowercase with no question mark; the converter capitalises them and adds one.
- Passages run 77 to 3,034 characters (median 474). The longest is 666 tokens, which breaks `llama-server` at its default batch size (see "Server settings").
- **Contamination:** BoolQ is public and popular, so models may have seen it in training. That can inflate accuracy and confidence, and not equally for every model. The fresh-news set exists to dodge this, so read BoolQ numbers alongside it.

## SMS Spam

- Source: UCI SMS Spam Collection (5,574 messages, 747 spam). Licence: CC BY 4.0. Citation: Almeida, Gómez Hidalgo and Yamakami, "Contributions to the Study of SMS Spam Filtering", DocEng 2011. The Hugging Face mirror lists its licence as "unknown", so UCI is the cited source.
- Question: "Is this message spam?" with the message as the passage. Yes means spam.
- Sampling: 250 spam and 250 ham, seed 1 (stratified). **The natural mix is 13% spam**, so this set does not reflect a real inbox. It is balanced so a model cannot reach 87% by always answering No, and so the spam class has enough answers to fill the reliability bands.
- Messages run 7 to 255 characters.

## Fresh news

- Purpose: questions about events no model in the test can know, so the set measures calibration on new material and not recall. Each question carries a passage, because without one it would measure confidence under ignorance.
- The main events fall between 9 October and 9 November 2026; a few passages add background from earlier in 2026 (Starship Flight 14 in September, the AI safety talks from July). Retrieval date: 12 November 2026.
- How it was made: an AI assistant ran web searches on 18 topics (US midterms, the World Series, Artemis, the Fed, Brazil's runoff, Nobel prizes, box office, Ukraine talks, NFL Week 9, October disasters, FDA approvals, Starship, the UK Budget, NBA opening nights, Amazon and Intel earnings, OPEC+, Champions League matchday 4, and OpenAI releases). It wrote one 4–8 sentence passage per topic using only facts in the search results, then wrote 7–14 questions per passage, roughly 60% Yes. Each `source` field lists the URLs the passage came from. Passages are summaries, not article text, so no news text is republished.
- Where sources disagreed or looked mislabelled (for example 2025 articles dated 2026), the disputed fact was left out of the passage. Several searches (F1 title, earnings dates, Bitcoin and gold, COP31, Gaza, the Supreme Court tariff case, Japan–China) returned too little to use and were dropped.
- The answer is whatever the passage says, so this set tests reading fresh facts, not world knowledge. Search results are secondary sources, and some are aggregators. A passage can therefore be wrong about the world and still be a correct test item, but nothing here should be quoted as news.
- **Clusters:** questions about one story share a passage, so they are not independent. The story is in the id (`news-<story>-<nn>`). The analysis resamples whole stories for this set; resampling by question would understate the error bars.
- **Spot-check list (10%, seed 1):** `news-midterms-06 news-fed-04 news-brazil-02 news-brazil-04 news-brazil-10 news-nobel-07 news-boxoffice-07 news-ukraine-08 news-nfl-01 news-nfl-06 news-nfl-08 news-disasters-04 news-disasters-06 news-fda-10 news-nba-01 news-nba-03 news-nba-04 news-amazonintel-04 news-amazonintel-10 news-opec-01 news-ucl-02`. A reviewer should check each answer against its passage. Delete any bad question instead of editing it.
- **Spot-check done (13 November 2026):** all 21 labels were re-read against their passages by the AI assistant, then the maintainer reviewed the same table and approved it. All 21 held, so no question was deleted. The check is against the passage only, not the real world.
- Machine check: both Q4 models answered all 201 questions. d1-2B scored 99.5% and Gemma E2B 96%, and no question was missed by both. The nine questions either model missed were re-read against their passages and every label held. Accuracy was used only to catch mislabelled answers, never to choose questions.
- **Difficulty:** with a passage in hand, the set is easy. That leaves few wrong answers to calibrate on, which is why it sits alongside BoolQ, which is harder.

## Server settings

The runner needs `llama-server -b 2048 -ub 2048`. With the default 512-token physical batch, d1-2B rejects any prompt over 512 tokens (the longest BoolQ passage is 666) with HTTP 500: "input is too large to process. increase the physical batch size". Smoke-tested on the Mac with both models: 12 questions per model, including the two longest BoolQ passages, all answered once the batch size was raised.

## Checks run

- All three files pass `mkdata validate` (unique ids, two distinct options, answer is an option, one set name per file, source present).
- Smoke test of 12 questions per model (random BoolQ and SMS items plus the two longest passages) ran through an early prototype of the runner: d1-2B Q4_K_M via `systemone` and Gemma E2B Q4_K_M via `tokenprob`, with `answer_mass` at least 0.9999 for Gemma. Gemma answered boolq-0681 wrongly at 0.9998 confidence, an early sign of the overconfidence the full run later measured.
