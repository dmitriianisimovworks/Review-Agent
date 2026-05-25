package reviewshape

import (
	"strings"

	"technical-specification-review-agent/internal/domain"
)

func IssueFingerprint(finding domain.Finding) string {
	text := normalizedIssueText(finding)

	switch {
	case hasAll(text, "threshold") && hasAny(text, "refund", "возврат"):
		return "refund_threshold"
	case hasAll(text, "partial", "refund") || hasAll(text, "частич", "возврат"):
		return "partial_refund_rules"
	case hasAny(text, "конкур", "одноврем", "race", "atomic", "lock", "блокиров", "захват") && hasAny(text, "кейс", "case", "queue", "очеред"):
		return "concurrency_case_assignment"
	case hasAny(text, "audit", "истори") && hasAny(text, "идемпотент", "консистент", "атомар", "дублир", "транзак"):
		return "audit_consistency"
	case hasAny(text, "audit", "истори") && hasAny(text, "auth", "authoriz", "rbac", "неавториз", "permission", "privilege", "привил"):
		return "audit_access_control"
	case hasAny(text, "queue", "кейс", "case", "очеред") && hasAny(text, "auth", "authoriz", "rbac", "неавториз", "permission", "privilege", "привил"):
		return "queue_access_control"
	case hasAny(text, "роль", "прав", "permission", "privilege", "rbac", "authoriz", "auth") && hasAny(text, "user", "пользоват", "rbac", "authoriz", "auth"):
		return "roles_and_permissions"
	default:
		return fallbackFingerprint(finding)
	}
}

func ThemeTitle(finding domain.Finding) string {
	switch IssueFingerprint(finding) {
	case "refund_threshold", "partial_refund_rules":
		return "Refund и финансовые операции"
	case "concurrency_case_assignment":
		return "Конкурентный доступ и блокировки"
	case "audit_consistency", "audit_access_control":
		return "Аудит и история изменений"
	case "queue_access_control", "roles_and_permissions":
		return "Роли и права доступа"
	}

	switch finding.Category {
	case "security_risk":
		return "Безопасность и доступы"
	case "api_problem":
		return "API и интеграционные контракты"
	case "ux_problem", "frontend_risk":
		return "UX и поведение интерфейса"
	case "contradiction":
		return "Противоречия"
	case "ambiguity":
		return "Неоднозначные требования"
	case "technical_risk", "scalability_risk", "devops_risk":
		return "Технические и интеграционные риски"
	default:
		return "Пропущенные требования"
	}
}

func PreferredRole(fingerprint string) string {
	switch fingerprint {
	case "refund_threshold":
		return string(domain.ReviewerRoleTechLead)
	case "partial_refund_rules":
		return string(domain.ReviewerRoleSeniorBackendEngineer)
	case "concurrency_case_assignment":
		return string(domain.ReviewerRoleSeniorBackendEngineer)
	case "audit_consistency":
		return string(domain.ReviewerRoleSeniorBackendEngineer)
	case "audit_access_control", "queue_access_control", "roles_and_permissions":
		return string(domain.ReviewerRoleSecurityLead)
	default:
		return ""
	}
}

func normalizedIssueText(finding domain.Finding) string {
	return strings.ToLower(strings.Join([]string{
		finding.Problem,
		finding.WhyItIsBad,
		finding.HowToFix,
		finding.SectionTitle,
		finding.Category,
	}, " "))
}

func fallbackFingerprint(finding domain.Finding) string {
	normalized := normalizeProblemKey(finding.Problem)
	if normalized == "" {
		normalized = normalizeProblemKey(finding.WhyItIsBad)
	}
	return ThemeTitleFallbackKey(finding) + "|" + normalized
}

func ThemeTitleFallbackKey(finding domain.Finding) string {
	return strings.ToLower(strings.ReplaceAll(ThemeTitleByCategoryOnly(finding), " ", "_"))
}

func ThemeTitleByCategoryOnly(finding domain.Finding) string {
	switch finding.Category {
	case "security_risk":
		return "Безопасность и доступы"
	case "api_problem":
		return "API и интеграционные контракты"
	case "ux_problem", "frontend_risk":
		return "UX и поведение интерфейса"
	case "contradiction":
		return "Противоречия"
	case "ambiguity":
		return "Неоднозначные требования"
	case "technical_risk", "scalability_risk", "devops_risk":
		return "Технические и интеграционные риски"
	default:
		return "Пропущенные требования"
	}
}

func normalizeProblemKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("\n", " ", "\t", " ", ",", " ", ".", " ", ":", " ", ";", " ", "(", " ", ")", " ", "`", " ")
	value = replacer.Replace(value)
	value = strings.Join(strings.Fields(value), "_")
	prefixes := []string{
		"отсутствует_описание_",
		"не_описано_",
		"не_указано_",
		"не_определено_",
		"не_конкретизировано_",
		"не_описан_",
		"не_описаны_",
		"отсутствует_",
		"отсутствуют_",
	}
	for _, prefix := range prefixes {
		value = strings.TrimPrefix(value, prefix)
	}
	parts := strings.Split(value, "_")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", "механизм", "описание", "требования", "требование", "явно", "системы", "система", "поведение":
			continue
		default:
			filtered = append(filtered, part)
		}
	}
	if len(filtered) > 10 {
		filtered = filtered[:10]
	}
	return strings.Join(filtered, "_")
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func hasAll(text string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}
