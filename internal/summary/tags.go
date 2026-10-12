package summary

// ValidAutoTags rejects invented labels and duplicates rather than saving them.
func ValidAutoTags(tags, vocabulary []string) bool {
	if len(tags) < 2 || len(tags) > 3 {
		return false
	}
	seen := make(map[string]bool)
	for _, tag := range tags {
		allowed := false
		for _, candidate := range vocabulary {
			if tag == candidate {
				allowed = true
				break
			}
		}
		if !allowed || seen[tag] {
			return false
		}
		seen[tag] = true
	}
	return true
}
