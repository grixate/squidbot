package skills

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/grixate/squidbot/internal/config"
)

var explicitMentionPattern = regexp.MustCompile(`\$([A-Za-z0-9][A-Za-z0-9_-]*)`)

type scoredSkill struct {
	Skill     SkillDescriptor
	Score     int
	Explicit  bool
	MatchedBy []string
	Breakdown ScoreBreakdown
}

func routeSkills(req ActivationRequest, snapshot IndexSnapshot, cfg config.Config) ActivationResult {
	result := ActivationResult{
		Activated: []SkillActivation{},
		Skipped:   []SkillSkip{},
		Warnings:  append([]string(nil), snapshot.Warnings...),
		Errors:    []string{},
		Diagnostics: ActivationDiagnostics{
			Explicit: []string{},
			Ranked:   []SkillRanked{},
		},
	}
	query := strings.TrimSpace(req.Query)
	explicit := collectExplicitMentions(query, req.ExplicitMentions)
	result.Diagnostics.Explicit = append(result.Diagnostics.Explicit, explicit...)

	validSkills := make([]SkillDescriptor, 0, len(snapshot.Skills))
	invalidByName := map[string]SkillDescriptor{}
	for _, skill := range snapshot.Skills {
		if !skill.Valid {
			invalidByName[strings.ToLower(skill.Name)] = skill
			invalidByName[strings.ToLower(skill.ID)] = skill
			continue
		}
		validSkills = append(validSkills, skill)
	}
	allowedSkills, deniedReasons := applyPolicyFilter(cfg, req.Channel, validSkills)

	deniedByName := map[string]string{}
	for _, skill := range validSkills {
		if reason, ok := deniedReasons[skill.ID]; ok {
			deniedByName[strings.ToLower(skill.Name)] = reason
			deniedByName[strings.ToLower(skill.ID)] = reason
		}
	}

	for _, mention := range explicit {
		if _, ok := invalidByName[strings.ToLower(mention)]; ok {
			result.Errors = append(result.Errors, formatExplicitError(mention, "invalid", snapshot.Skills))
			continue
		}
		if reason, denied := deniedByName[strings.ToLower(mention)]; denied {
			result.Errors = append(result.Errors, formatExplicitError(mention, reason, snapshot.Skills))
			continue
		}
		if !skillExistsByMention(allowedSkills, mention) {
			result.Errors = append(result.Errors, formatExplicitError(mention, "not_found", snapshot.Skills))
		}
	}
	if len(result.Errors) > 0 {
		return result
	}

	scored := make([]scoredSkill, 0, len(allowedSkills))
	for _, skill := range allowedSkills {
		score, breakdown, matchedBy, isExplicit := scoreSkill(skill, query, explicit)
		if score == 0 && !isExplicit {
			continue
		}
		scored = append(scored, scoredSkill{Skill: skill, Score: score, Explicit: isExplicit, MatchedBy: matchedBy, Breakdown: breakdown})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Skill.ID < scored[j].Skill.ID
		}
		return scored[i].Score > scored[j].Score
	})

	threshold := maxInt(cfg.Skills.MatchThreshold, 1)
	maxActive := maxInt(cfg.Skills.MaxActive, 1)
	for _, entry := range scored {
		reason := "score"
		status := "candidate"
		if entry.Explicit {
			reason = "explicit"
		} else if containsString(entry.MatchedBy, "id") || containsString(entry.MatchedBy, "name") {
			reason = "name"
		} else if containsString(entry.MatchedBy, "tag") {
			reason = "tag"
		}
		if len(result.Activated) >= maxActive {
			result.Skipped = append(result.Skipped, SkillSkip{ID: entry.Skill.ID, Name: entry.Skill.Name, Reason: "max_active_reached", Score: entry.Score})
			status = "skipped"
			reason = "max_active_reached"
			result.Diagnostics.Ranked = append(result.Diagnostics.Ranked, SkillRanked{
				ID: entry.Skill.ID, Name: entry.Skill.Name, Score: entry.Score, Explicit: entry.Explicit,
				MatchedBy: append([]string(nil), entry.MatchedBy...), Status: status, Reason: reason, Breakdown: entry.Breakdown,
			})
			continue
		}
		if !entry.Explicit && entry.Score < threshold {
			result.Skipped = append(result.Skipped, SkillSkip{ID: entry.Skill.ID, Name: entry.Skill.Name, Reason: "below_threshold", Score: entry.Score})
			status = "skipped"
			reason = "below_threshold"
			result.Diagnostics.Ranked = append(result.Diagnostics.Ranked, SkillRanked{
				ID: entry.Skill.ID, Name: entry.Skill.Name, Score: entry.Score, Explicit: entry.Explicit,
				MatchedBy: append([]string(nil), entry.MatchedBy...), Status: status, Reason: reason, Breakdown: entry.Breakdown,
			})
			continue
		}
		result.Activated = append(result.Activated, SkillActivation{
			Skill:     SkillMaterialized{Descriptor: entry.Skill},
			Score:     entry.Score,
			Reason:    reason,
			Explicit:  entry.Explicit,
			MatchedBy: append([]string(nil), entry.MatchedBy...),
			Breakdown: entry.Breakdown,
		})
		status = "activated"
		result.Diagnostics.Ranked = append(result.Diagnostics.Ranked, SkillRanked{
			ID: entry.Skill.ID, Name: entry.Skill.Name, Score: entry.Score, Explicit: entry.Explicit,
			MatchedBy: append([]string(nil), entry.MatchedBy...), Status: status, Reason: reason, Breakdown: entry.Breakdown,
		})
	}

	result.Diagnostics.Matched = len(scored)
	for _, skill := range snapshot.Skills {
		if !skill.Valid {
			result.Diagnostics.InvalidSeen++
		}
	}
	return result
}

func collectExplicitMentions(query string, explicit []string) []string {
	mentions := make([]string, 0, 8)
	seen := map[string]struct{}{}
	for _, match := range explicitMentionPattern.FindAllStringSubmatch(query, -1) {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		mentions = append(mentions, name)
	}
	for _, item := range explicit {
		name := strings.TrimSpace(strings.TrimPrefix(item, "$"))
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		mentions = append(mentions, name)
	}
	sort.Strings(mentions)
	return mentions
}

func scoreSkill(skill SkillDescriptor, query string, explicit []string) (int, ScoreBreakdown, []string, bool) {
	breakdown := ScoreBreakdown{}
	matchedBy := []string{}
	lowerQuery := strings.ToLower(query)
	explicitMatch := skillMentioned(explicit, skill)
	if explicitMatch {
		breakdown.ExplicitBonus = 1000
		matchedBy = append(matchedBy, "explicit")
	}
	if containsPhrase(lowerQuery, strings.ToLower(skill.ID)) {
		breakdown.IDBonus = 130
		matchedBy = append(matchedBy, "id")
	}
	if containsPhrase(lowerQuery, strings.ToLower(skill.Name)) {
		breakdown.NameBonus = 120
		matchedBy = append(matchedBy, "name")
	}
	for _, alias := range skill.Aliases {
		if containsPhrase(lowerQuery, strings.ToLower(alias)) {
			breakdown.AliasBonus = 90
			matchedBy = append(matchedBy, "alias")
			break
		}
	}
	queryTokens := tokenSet(query)
	tagHits := 0
	for _, tag := range skill.Tags {
		if _, ok := queryTokens[strings.ToLower(tag)]; ok {
			tagHits++
		}
	}
	breakdown.TagHits = tagHits
	breakdown.TagBonus = 20 * tagHits
	if tagHits > 0 {
		matchedBy = append(matchedBy, "tag")
	}
	breakdown.DescriptionHits = overlapCount(queryTokens, tokenSet(skill.Description))
	breakdown.DescriptionBonus = minInt(breakdown.DescriptionHits*5, 40)
	exampleHits := 0
	for _, example := range skill.Examples {
		exampleHits += overlapCount(queryTokens, tokenSet(example))
	}
	breakdown.ExampleHits = exampleHits
	breakdown.ExampleBonus = minInt(exampleHits*8, 24)
	breakdown.Total = breakdown.ExplicitBonus + breakdown.IDBonus + breakdown.NameBonus + breakdown.AliasBonus + breakdown.TagBonus + breakdown.DescriptionBonus + breakdown.ExampleBonus

	if !explicitMatch && breakdown.IDBonus == 0 && breakdown.NameBonus == 0 && breakdown.AliasBonus == 0 && breakdown.TagBonus == 0 {
		if breakdown.DescriptionHits < 2 && breakdown.ExampleHits < 2 {
			breakdown.Total = minInt(breakdown.Total, 10)
		}
	}
	return breakdown.Total, breakdown, dedupeStrings(matchedBy), explicitMatch
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func skillMentioned(explicit []string, skill SkillDescriptor) bool {
	for _, mention := range explicit {
		lower := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(mention, "$")))
		if lower == "" {
			continue
		}
		if lower == strings.ToLower(skill.ID) || lower == strings.ToLower(skill.Name) {
			return true
		}
		for _, alias := range skill.Aliases {
			if lower == strings.ToLower(alias) {
				return true
			}
		}
	}
	return false
}

func containsPhrase(query string, phrase string) bool {
	phrase = strings.TrimSpace(strings.ToLower(phrase))
	if phrase == "" {
		return false
	}
	return strings.Contains(" "+normalizePhrase(query)+" ", " "+normalizePhrase(phrase)+" ")
}

func normalizePhrase(input string) string {
	input = strings.ToLower(input)
	builder := strings.Builder{}
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune(' ')
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

var stopWords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "to": {}, "for": {}, "of": {},
	"in": {}, "on": {}, "by": {}, "with": {}, "from": {}, "this": {}, "that": {},
	"please": {}, "help": {}, "do": {}, "make": {}, "run": {}, "use": {}, "need": {},
}

func tokenSet(input string) map[string]struct{} {
	norm := normalizePhrase(input)
	out := map[string]struct{}{}
	for _, token := range strings.Fields(norm) {
		if token == "" {
			continue
		}
		if len(token) <= 1 {
			continue
		}
		if _, stop := stopWords[token]; stop {
			continue
		}
		allDigits := true
		for _, r := range token {
			if !unicode.IsDigit(r) {
				allDigits = false
				break
			}
		}
		if allDigits {
			continue
		}
		out[token] = struct{}{}
	}
	return out
}

func overlapCount(a, b map[string]struct{}) int {
	count := 0
	for token := range a {
		if _, ok := b[token]; ok {
			count++
		}
	}
	return count
}

func skillExistsByMention(skills []SkillDescriptor, mention string) bool {
	needle := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(mention, "$")))
	for _, skill := range skills {
		if needle == strings.ToLower(skill.ID) || needle == strings.ToLower(skill.Name) {
			return true
		}
		for _, alias := range skill.Aliases {
			if needle == strings.ToLower(alias) {
				return true
			}
		}
	}
	return false
}

func formatExplicitError(name, reason string, skills []SkillDescriptor) string {
	suggestions := suggestSkills(name, skills, 5)
	details := ""
	if len(suggestions) > 0 {
		details = "; try: " + strings.Join(suggestions, ", ")
	}
	return fmt.Sprintf("explicit skill %q failed (%s)%s", name, reason, details)
}

func suggestSkills(name string, skills []SkillDescriptor, limit int) []string {
	type candidate struct {
		name string
		dist int
	}
	needle := strings.ToLower(strings.TrimSpace(name))
	candidates := make([]candidate, 0, len(skills))
	seen := map[string]struct{}{}
	for _, skill := range skills {
		if !skill.Valid {
			continue
		}
		label := skill.Name
		if _, ok := seen[strings.ToLower(label)]; ok {
			continue
		}
		seen[strings.ToLower(label)] = struct{}{}
		candidates = append(candidates, candidate{name: label, dist: editDistance(needle, strings.ToLower(label))})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].dist == candidates[j].dist {
			return candidates[i].name < candidates[j].name
		}
		return candidates[i].dist < candidates[j].dist
	})
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, candidates[i].name)
	}
	return out
}

func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			ins := curr[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			curr[j] = minInt(ins, minInt(del, sub))
		}
		prev = curr
	}
	return prev[len(b)]
}

func containsString(values []string, item string) bool {
	for _, value := range values {
		if value == item {
			return true
		}
	}
	return false
}
