package usecase

// SessionsToGC returns the review-ref session IDs, from refSessions, whose
// store directory is gone from storedSessions — their logs no longer exist,
// so their throwaway review ref is pure debris, safe to delete (SPEC §7).
// Order follows refSessions.
func SessionsToGC(refSessions, storedSessions []string) []string {
	stored := make(map[string]bool, len(storedSessions))
	for _, s := range storedSessions {
		stored[s] = true
	}

	var gone []string
	for _, id := range refSessions {
		if !stored[id] {
			gone = append(gone, id)
		}
	}
	return gone
}
