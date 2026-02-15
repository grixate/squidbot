package skills

import (
	"strings"

	"github.com/grixate/squidbot/internal/config"
)

type policyDecision struct {
	Allowed bool
	Reason  string
}

func applyPolicyFilter(cfg config.Config, channel string, skills []SkillDescriptor) (allowed []SkillDescriptor, denied map[string]string) {
	allowed = make([]SkillDescriptor, 0, len(skills))
	denied = map[string]string{}
	for _, skill := range skills {
		decision := evaluatePolicy(cfg, channel, skill)
		if decision.Allowed {
			allowed = append(allowed, skill)
			continue
		}
		denied[skill.ID] = decision.Reason
	}
	sortDescriptors(allowed)
	return allowed, denied
}

func evaluatePolicy(cfg config.Config, channel string, skill SkillDescriptor) policyDecision {
	channel = strings.ToLower(strings.TrimSpace(channel))
	globalAllow := normalizePolicyList(cfg.Skills.Policy.Allow)
	globalDeny := normalizePolicyList(cfg.Skills.Policy.Deny)
	channelPolicy := cfg.Skills.Policy.Channels[channel]
	channelAllow := normalizePolicyList(channelPolicy.Allow)
	channelDeny := normalizePolicyList(channelPolicy.Deny)

	if len(globalAllow) > 0 && !matchesPolicyTokenSet(globalAllow, skill) {
		return policyDecision{Allowed: false, Reason: "denied_by_global_allowlist"}
	}
	if len(globalDeny) > 0 && matchesPolicyTokenSet(globalDeny, skill) {
		return policyDecision{Allowed: false, Reason: "denied_by_global"}
	}
	if len(channelAllow) > 0 && !matchesPolicyTokenSet(channelAllow, skill) {
		return policyDecision{Allowed: false, Reason: "denied_by_channel_allowlist"}
	}
	if len(channelDeny) > 0 && matchesPolicyTokenSet(channelDeny, skill) {
		return policyDecision{Allowed: false, Reason: "denied_by_channel"}
	}
	return policyDecision{Allowed: true}
}

func normalizePolicyList(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		out[trimmed] = struct{}{}
	}
	return out
}

func matchesPolicyTokenSet(set map[string]struct{}, skill SkillDescriptor) bool {
	if len(set) == 0 {
		return false
	}
	candidates := []string{strings.ToLower(skill.ID), strings.ToLower(skill.Name)}
	for _, alias := range skill.Aliases {
		candidates = append(candidates, strings.ToLower(strings.TrimSpace(alias)))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := set[candidate]; ok {
			return true
		}
	}
	return false
}
