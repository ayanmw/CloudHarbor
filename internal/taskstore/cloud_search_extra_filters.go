package taskstore

import "strings"

func (f CloudInventorySearchFilter) validExtraFilters() bool {
	const maxExclusive = int64(253402300800) // 10000-01-01 UTC, beyond the last supported date.
	if f.ModifiedFrom < 0 || f.ModifiedBefore < 0 || f.ModifiedFrom >= maxExclusive || f.ModifiedBefore > maxExclusive || (f.ModifiedFrom != 0 && f.ModifiedBefore != 0 && f.ModifiedFrom >= f.ModifiedBefore) {
		return false
	}
	if f.Extensions == "" {
		return true
	}
	if f.Kind == "directory" {
		return false
	}
	parts := strings.Split(f.Extensions, ",")
	if len(parts) > 10 {
		return false
	}
	seen := map[string]bool{}
	for _, part := range parts {
		if len(part) < 1 || len(part) > 16 || seen[part] {
			return false
		}
		for _, ch := range part {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9') {
				return false
			}
		}
		seen[part] = true
	}
	return true
}
