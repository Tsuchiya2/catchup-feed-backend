package pagination

// CalculateOffset calculates the database OFFSET value based on page number and limit.
// Page numbers are 1-based, so page 1 has offset 0.
//
// Formula: offset = (page - 1) * limit
//
// Examples:
//   - Page 1, Limit 20 -> Offset 0
//   - Page 2, Limit 20 -> Offset 20
//   - Page 3, Limit 10 -> Offset 20
func CalculateOffset(page, limit int) int {
	return (page - 1) * limit
}

// CalculateTotalPages calculates the total number of pages based on total items and limit.
// Uses ceiling division to ensure all items are included.
//
// Special cases:
//   - If total is 0, returns 1 (always at least 1 page)
//   - If total < limit, returns 1
//   - Otherwise, returns ceil(total / limit)
//
// Examples:
//   - Total 0, Limit 20 -> 1 page
//   - Total 10, Limit 20 -> 1 page
//   - Total 20, Limit 20 -> 1 page
//   - Total 21, Limit 20 -> 2 pages
//   - Total 100, Limit 20 -> 5 pages
func CalculateTotalPages(total int64, limit int) int {
	if total == 0 {
		return 1 // Always at least 1 page
	}
	// Ceiling division: (total + limit - 1) / limit
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	return totalPages
}
