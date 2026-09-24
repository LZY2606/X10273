package app

import (
	"ifdroom/internal/derive"
	"ifdroom/internal/model"
)

func paramsFromPayload(p map[string]any) derive.Params {
	par := derive.DefaultParams()
	getInt := func(k string) (int, bool) {
		v, ok := p[k]
		if !ok {
			return 0, false
		}
		switch n := v.(type) {
		case float64:
			return int(n), true
		case int:
			return n, true
		}
		return 0, false
	}
	if v, ok := getInt("ifdBudget"); ok {
		par.IfdBudget = v
	}
	if v, ok := getInt("valueBudget"); ok {
		par.ValueBudget = v
	}
	return par
}

func paramsPayload(p derive.Params) map[string]any {
	return map[string]any{"ifdBudget": p.IfdBudget, "valueBudget": p.ValueBudget,
		"fingerprint": p.Fingerprint(), "algorithmVersion": derive.AlgorithmVersion}
}

func trialFromEvent(e model.Event) (model.Trial, bool) {
	var t model.Trial
	p := e.Payload
	str := func(k string) string {
		if v, ok := p[k].(string); ok {
			return v
		}
		return ""
	}
	t.ID = str("trialId")
	if t.ID == "" {
		return t, false
	}
	t.EventID = e.ID
	t.Name = str("name")
	t.Code = str("code")
	t.BaselineVer = str("baselineVersionId")
	t.MutatedVer = str("mutatedVersionId")
	t.Mutation = str("mutation")
	t.ParamChange = str("paramChange")
	t.Verdict = str("verdict")
	t.Standing = str("standing")
	t.Contained, _ = p["impactContained"].(bool)
	t.ScopeClaim = anyStrings(p["expectedAffected"])
	t.Observed = anyStrings(p["observedAffected"])
	t.Evidence = anyStrings(p["evidence"])
	return t, true
}

func decisionFromEvent(e model.Event) (model.Decision, bool) {
	p := e.Payload
	id, _ := p["decisionId"].(string)
	if id == "" {
		return model.Decision{}, false
	}
	return model.Decision{
		ID: id, EventID: e.ID,
		TargetKind: anyString(p, "targetKind"), TargetID: anyString(p, "targetId"),
		Verdict: anyString(p, "verdict"), Reason: anyString(p, "reason"),
		Reviewer: anyString(p, "reviewer"), Time: e.Time, Active: true,
	}, true
}

func anyString(p map[string]any, k string) string {
	if v, ok := p[k].(string); ok {
		return v
	}
	return ""
}

func anyStrings(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func stringsToAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func criteriaToAny(cs []model.CriterionEvent) []any {
	out := make([]any, len(cs))
	for i, c := range cs {
		out[i] = map[string]any{
			"rule": c.Rule, "ruleVersion": c.RuleVersion, "statement": c.Statement,
			"subjects": stringsToAny(c.Subjects), "holds": c.Holds,
			"evidence": stringsToAny(c.Evidence),
		}
	}
	return out
}

func criteriaFromAny(v any, eventID string) []model.CriterionEvent {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []model.CriterionEvent
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		c = model.CriterionEvent{EventID: eventID,
			Rule:        anyString(m, "rule"),
			RuleVersion: anyString(m, "ruleVersion"),
			Statement:   anyString(m, "statement"),
			Subjects:    anyStrings(m["subjects"]),
			Evidence:    anyStrings(m["evidence"]),
		}
		c.Holds, _ = m["holds"].(bool)
		out = append(out, c)
	}
	return out
}
