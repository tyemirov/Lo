package value

// FileLeases finds retained observation handles in immutable live values.
func FileLeases(values ...Value) []FileLease {
	seen := map[FileLease]bool{}
	var result []FileLease
	Visit(values, func(current Value) {
		var lease FileLease
		switch item := current.(type) {
		case FileEntry:
			lease = item.Observation().Lease
		case FileDirectoryObservation:
			lease = item.Observation().Lease
		}
		if lease.token != nil && !seen[lease] {
			seen[lease] = true
			result = append(result, lease)
		}
	})
	return result
}
