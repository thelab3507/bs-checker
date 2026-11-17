# Do decision models deliver honest confidence? First results from BS Meter

A new kind of model answers a yes/no question and attaches a confidence number: "Yes, 92% sure". People are starting to build automation on that number ("if it is over 90% sure, approve it; otherwise ask a human"). That only works if the number is honest: when a model says 92%, it should be right about 92% of the time.

The idea for this project came from a Hightouch post, [LLM judges for identity resolution](https://hightouch.com/blog/llm-judges-identity-resolution). It credits the edge of TypeSafe's Jev, a closed, paid decision model, to its calibrated confidence: its mistakes come with low confidence, so the uncertain middle can be escalated to a bigger model. Jev costs money, so it is not tested here. BS Meter tests the open decision models that run on a laptop, and asks whether their confidence numbers are honest.

This is what it found on its first run, on three small models.

## The answers

1. **The two decision models do not overstate their confidence. If anything they understate it.** Liquid d1-2B is 5 to 6 points underconfident on both of the sets big enough to judge. Kev-4B is 2 points under on BoolQ (the interval just spans zero, so no miscalibration is detected) and 13 points under on SMS spam.
2. **The ordinary chat model forced to say Yes or No is badly overconfident.** Gemma 4 E2B, used as the control, claims 95% or more on 533 of its 600 BoolQ answers and is right on 75% of them.
3. **Underconfidence is the cheaper failure.** An underconfident model wastes escalations; an overconfident one lets wrong answers through. But it can still break a rule: Kev never reached 95% on any SMS spam answer, so a "95% and up is auto-approved" rule would never fire on that set.

## 1. When a model is confident, is it correct?

d1 and Kev get their confidence the same way, through llama.cpp's `/v1/systemone` endpoint, so they can be compared with each other. Gemma gets it a different way (see below), so it is a control and not a rival. ECE is expected calibration error, the average gap between how sure a model said it was and how often it was right. The gap is confidence minus accuracy, so a positive number means overconfident. Everything is Q4_K_M.

| Model | Set | Accuracy | ECE (points) | Gap (points) | Answers at 95%+ confidence | Right when 95%+ | Verdict |
| --- | --- | --- | --- | --- | --- | --- | --- |
| d1-2B | BoolQ | 88.8% | 4.9 | −4.9 (−7.1 to −2.3) | 173 | 98.8% | underconfident |
| d1-2B | SMS spam | 93.2% | 5.9 | −5.8 (−7.9 to −3.7) | 177 | 98.3% | underconfident |
| d1-2B | fresh-news | 99.5% | 3.7 | −3.5 (−4.7 to −2.2) | 151 | 100% | too few to judge |
| Kev-4B | BoolQ | 90.8% | 3.5 | −2.0 (−4.0 to +0.1) | 285 | 98.6% | honest |
| Kev-4B | SMS spam | 85.6% | 13.3 | −13.3 (−15.8 to −10.4) | 0 | none | underconfident |
| Kev-4B | fresh-news | 99.5% | 5.3 | −5.3 (−6.8 to −3.6) | 146 | 100% | too few to judge |
| Gemma E2B | BoolQ | 72.2% | 25.5 | +25.0 (+21.5 to +28.5) | 533 | 75.2% | overconfident |
| Gemma E2B | SMS spam | 89.0% | 10.1 | +9.6 (+7.1 to +12.4) | 472 | 90.7% | overconfident |
| Gemma E2B | fresh-news | 96.0% | 3.7 | +3.3 (+1.5 to +5.3) | 194 | 98.5% | too few to judge |

"Honest" here only means the tool could not detect a gap: Kev's BoolQ interval ends 0.1 point above zero.

- **Gemma is badly overconfident.** On BoolQ an auto-approve rule at 95% would let through one wrong answer in four (132 of 533). About 70% of all its answers, across the three sets, sit at 99.99% confidence or higher.
- **d1 is a little underconfident**, by about 5 points. When it claims 95% or more it is right 98% to 99% of the time on the two big sets.
- **Kev is a mixed case.** It is the best of the three on BoolQ and the worst of the decision models on SMS spam. On SMS spam it is right 85.6% of the time but never says it is more than 95% sure, and its average confidence is 13 points below its accuracy. Its mistakes there do come with lower confidence than its right answers (59% on average against 74%), which is the property Hightouch credits Jev with, but its numbers are too modest to act on at a high threshold.
- **There is no winner between d1 and Kev.** Kev is better calibrated on BoolQ and d1 is better on SMS spam. Two models and three sets don't support a ranking.
- **fresh-news gets no verdict.** It has 201 questions but only 18 independent stories, because questions about one story share a passage and are not independent. The rule, written down before the first full run, needs at least 30 groups. The numbers are shown for completeness.

### A simple correction

Temperature scaling, fitted on a random half of each set and tested on the other half, cuts the held-out ECE where there is something to fix:

| Run | Held-out ECE before | After |
| --- | --- | --- |
| Gemma BoolQ | 0.278 | 0.071 |
| Gemma SMS spam | 0.105 | 0.049 |
| Kev SMS spam | 0.138 | 0.047 |
| d1 SMS spam | 0.068 | 0.047 |

Where a run was already close to honest, the correction does little or makes it slightly worse: Kev's BoolQ goes from 0.030 to 0.035 and Gemma's fresh-news from 0.030 to 0.043. The fitted parameters are in `results/corrections-mac-metal/`. The fitted temperature is above 1 for Gemma (softer) and below 1 for the decision models (sharper), matching the direction of each model's gap.

## How it was measured

- **Questions with known answers.** BoolQ (600 yes/no reading questions), the SMS Spam Collection (500 messages, spam or not) and fresh-news (201 questions about events from October and November 2026, each with its passage, so the answer is whatever the passage says).
- **Two ways to read confidence.** d1 and Kev are purpose-built decision models: llama.cpp's `/v1/systemone` endpoint returns a probability for each option. Gemma is an ordinary chat model, so it is given the passage and question and asked to answer with one word, Yes or No, with thinking turned off. The probabilities of the Yes and No tokens are read and rescaled to sum to 1. In both cases the confidence is the probability of the answer the model chose, so it is always between 50% and 100%.
- **Comparing claims with outcomes.** Answers are grouped into bands by confidence, and each band's average claimed confidence is compared with how often the model was right.
- **Error bars.** Everything has a 95% interval from 1,000 bootstrap resamples. Fresh-news is resampled by story.
- **A rule fixed in advance.** Before the first full run, the project wrote down what would count as "overconfident", "underconfident", "honest" and "too few answers to judge" (see "How verdicts are decided" in the README). The verdict comes from the interval of the signed gap.
- **Kev was added later.** d1 and Gemma were run first. Kev was added after those results were seen, with the same rules, the same questions and the same settings, and nothing was tuned for it.

One MacBook Pro, one llama.cpp build (12110, Metal), 3,903 analysed answers (1,301 per model). The mean time per answer was 37 milliseconds for d1, 39 for Gemma and 91 for Kev. d1 and Gemma ran on 13 November 2026 and Kev on 16 November. Full provenance, including model file hashes, is in [`results/README.md`](../results/README.md).

## What went wrong or changed along the way

- **Models were dropped or left out.** Clef-Flash was parked: its Q4_K_M file is 6.73 GB, a marginal fit for an 8 GB graphics card. Gemma E4B was dropped. Jev was left out because it costs money.
- **d1 rejected long prompts.** At llama.cpp's default batch size, d1-2B returns an error for any prompt over 512 tokens. The longest BoolQ passage is 666. The runner now launches the server with `-b 2048 -ub 2048`. Kev ran on those same default flags with nothing extra.
- **ECE has no sign.** It says a model is miscalibrated but not whether it is over- or underconfident. A signed gap (average confidence minus accuracy) was added after the first 20-answer smoke test and before the full run, so that "overconfident" and "underconfident" could be said honestly. The verdicts come from that gap.
- **fresh-news was too small to judge.** 201 questions looked like enough, but they come in 18 stories. The rule held and gave no verdict.
- **An early impression was wrong.** The first 20 easy questions made Gemma look nearly perfect. On the real BoolQ set it is 72% right while still extremely sure of itself: 62% of its BoolQ answers, and 85% of its fresh-news answers, sit at 99.99% confidence or higher. A baseline figure quoted during development also came from different questions than the later check, and was corrected.
- **Windows needed a fix and a newer llama.cpp.** Three of the project's own tests asserted Unix error wording and failed on Windows; they now check the file path instead. The PC's llama.cpp (b10696) was updated to b12344 before the checks, because d1 support was not guaranteed in the older build.
- **Some news topics were dropped.** Searches for several fresh-news topics (the F1 title race, Bitcoin and gold, COP31, Gaza and others) returned too little to build reliable questions.

## Limits

- **Three small models, one machine.** Gemma 4 E2B, Liquid d1-2B and Kev-4B, each as one Q4_K_M file, on one MacBook Pro with one llama.cpp build. Other sizes, other quantisations and other machines may differ.
- **Jev was not tested.** It is closed and paid, and the Hightouch post's claims about it are not checked here. These results say nothing about how well calibrated it is.
- **Not a ranking.** d1 and Kev share a confidence mechanism but beat each other on different sets, and Gemma uses a different mechanism, so "d1 is better calibrated than Gemma" is not something these results support.
- **Honesty is not accuracy.** A model can be perfectly calibrated and still often wrong, if it says so. This measures whether the number can be trusted, not whether the answer is right.
- **Easy questions, few errors.** The decision models are wrong so rarely at high confidence (0 to 4 wrong answers in each 95%+ group of 146 or more) that their danger-zone numbers are imprecise.
- **fresh-news is machine-written and checked against its passages, not the world.** An AI assistant wrote the passages from web searches and then the questions. A passage can be wrong about the real world and still be a valid test item. The set measures reading, not knowledge, and it isn't news.
- **BoolQ is public.** The models may have seen it in training, and not equally.
- **The correction is local.** The fitted temperature is tied to the question mix it was fitted on. It is not a general fix for a model.

## Future work

- **Jev**, if a cheap one-off run is worth paying for.
- **Clef-Flash**, a marginal fit for the 8 GB card this was built for (6.73 GB at Q4_K_M).
- A rephrasing test (does confidence swing across three phrasings of the same question?) and an "I don't know" test (does confidence drop on unanswerable questions?).

## Run it yourself

```
go run ./cmd/bsmeter run -host my-machine -out my-results.jsonl
go run ./cmd/bsmeter analyze -results my-results.jsonl -out my-analysis.json
go run ./cmd/bsmeter report -analysis my-analysis.json -out my-report.html
```

You need Go, a recent `llama-server` with `/v1/systemone` support (build 12110 on the Mac and 12344 on Windows were used) and the model files, which `llama-server` downloads. The model list and your own question files are plain JSON and JSONL: see the [README](../README.md). To check the published numbers, follow the steps in [`results/README.md`](../results/README.md).

Code is MIT-licensed. The BoolQ questions are CC BY-SA 3.0 and the SMS Spam messages are CC BY 4.0; see `data/README.md`.
