package reviewshape

import (
	"strings"

	"technical-specification-review-agent/internal/domain"
)

func IssueFingerprint(finding domain.Finding) string {
	text := normalizedIssueText(finding)

	switch {
	case hasAny(text, "threshold", "порог") && hasAny(text, "refund", "возврат"):
		return "refund_threshold"
	case hasAll(text, "partial", "refund") || hasAll(text, "частич", "возврат"):
		return "partial_refund_rules"
	case hasAny(text, "комментар", "comment", "notes", "заметк") &&
		hasAny(text, "редакт", "удал", "истори", "упомин", "mention", "лимит", "размер"):
		return "internal_comment_lifecycle"
	case hasAny(text, "справочник", "dictionary", "reason", "причин") && hasAny(text, "обязат", "mandatory", "required", "действ", "action"):
		return "reason_dictionary_rules"
	case hasAny(text, "конкур", "одноврем", "race", "atomic", "lock", "блокиров", "захват") && hasAny(text, "кейс", "case", "queue", "очеред"):
		return "concurrency_case_assignment"
	case hasAny(text, "audit", "истори") && hasAny(text, "идемпотент", "консистент", "атомар", "дублир", "транзак"):
		return "audit_consistency"
	case hasAny(text, "audit", "истори") && hasAny(text, "auth", "authoriz", "rbac", "неавториз", "permission", "privilege", "привил"):
		return "audit_access_control"
	case hasAny(text, "идемпотент", "idempot") && hasAny(text, "bill", "billing", "subscription", "подпис", "инвойс", "invoice", "payment", "платеж"):
		return "billing_idempotency"
	case hasAny(text, "load", "loading", "empty", "error", "ошиб", "пуст", "загруз") && hasAny(text, "ui", "ux", "интерф", "screen", "state", "состояни"):
		return "loading_empty_error_states"
	case hasAny(text, "retry", "повтор", "повторн", "redelivery", "backoff") && hasAny(text, "notification", "notify", "уведом", "webhook", "delivery", "доставк"):
		return "notification_retry"
	case hasAny(text, "sla", "slo", "latency", "uptime", "доступност", "deadline") && hasAny(text, "monitor", "alert", "метрик", "контрол", "эскалац", "violation"):
		return "sla_slo_requirements"
	case hasAny(text, "export", "report", "csv", "xlsx", "отчет", "выгруз", "экспорт"):
		return "report_export_rules"
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
	case "internal_comment_lifecycle":
		return "Комментарии и lifecycle"
	case "reason_dictionary_rules":
		return "Причины и справочники действий"
	case "concurrency_case_assignment":
		return "Конкурентный доступ и блокировки"
	case "audit_consistency", "audit_access_control":
		return "Аудит и история изменений"
	case "billing_idempotency":
		return "Идемпотентность и биллинг"
	case "loading_empty_error_states":
		return "UX и состояния интерфейса"
	case "notification_retry":
		return "Уведомления и retry"
	case "sla_slo_requirements":
		return "SLA, SLO и эксплуатационные требования"
	case "report_export_rules":
		return "Отчёты и экспорт"
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
	case "internal_comment_lifecycle":
		return string(domain.ReviewerRoleQAReviewer)
	case "reason_dictionary_rules":
		return string(domain.ReviewerRoleQAReviewer)
	case "concurrency_case_assignment":
		return string(domain.ReviewerRoleSeniorBackendEngineer)
	case "audit_consistency":
		return string(domain.ReviewerRoleSeniorBackendEngineer)
	case "billing_idempotency":
		return string(domain.ReviewerRoleSeniorBackendEngineer)
	case "loading_empty_error_states":
		return string(domain.ReviewerRoleSeniorFrontendEngineer)
	case "notification_retry":
		return string(domain.ReviewerRoleDevOpsReviewer)
	case "sla_slo_requirements":
		return string(domain.ReviewerRoleDevOpsReviewer)
	case "report_export_rules":
		return string(domain.ReviewerRoleQAReviewer)
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
