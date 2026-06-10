package main

var allowedSessionTransitions = map[string]map[string]struct{}{
	"pending": {
		"pending":     {},
		"in_progress": {},
	},
	"in_progress": {
		"in_progress": {},
		"review":      {},
		"pending":     {},
	},
	"review": {
		"review":      {},
		"completed":   {},
		"pending":     {},
		"in_progress": {},
	},
	"completed": {
		"completed":   {},
		"pending":     {},
		"in_progress": {},
		"review":      {},
	},
}

func isValidSessionStatus(status string) bool {
	_, exists := validSessionStatuses[status]
	return exists
}

func isAllowedSessionTransition(fromStatus, toStatus string) bool {
	allowedTargets, exists := allowedSessionTransitions[fromStatus]
	if !exists {
		return false
	}
	_, allowed := allowedTargets[toStatus]
	return allowed
}
