package vizevalfacts

import "time"

// VizevalJudgement is one pairwise comparison as a `boxer.facts` row
// (ADR-0257 §SD6, §SD8): two candidates' drawings compared by a model under a
// prompt version, both orders merged. Id is the xxh3 of NaturalKey, which
// names what makes two comparisons the same — scenario, prompt, model and the
// two drawings — so a comparison asked again lands under the same key.
type VizevalJudgement struct {
	_ struct{} `kind:"vizevalJudgement"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind        string `lw:"runtimeKindVizevalJudgement,symbol"`
	Scenario    string `lw:"vizevalJudgementScenario,symbol"`
	BatchDigest string `lw:"vizevalJudgementDigest,symbol"`
	// Model and Prompt say who judged under which instructions; judgements
	// by different models or prompts are different measurements.
	Model  string `lw:"vizevalJudgeModel,symbol"`
	Prompt string `lw:"vizevalJudgePrompt,symbol"`
	// A and B are candidate ids; DrawingA and DrawingB the digests of what
	// they drew, the identity the comparison was of.
	A        string `lw:"vizevalPairA,symbol"`
	B        string `lw:"vizevalPairB,symbol"`
	DrawingA string `lw:"vizevalDrawingA,symbol"`
	DrawingB string `lw:"vizevalDrawingB,symbol"`
	// Criterion, Preference and Why are parallel: the i-th preference and
	// reason are the i-th criterion's.
	Criterion  []string `lw:"vizevalCriterion,symbolArray"`
	Preference []string `lw:"vizevalPreference,symbolArray"`
	Why        []string `lw:"vizevalWhy,stringArray"`
}
