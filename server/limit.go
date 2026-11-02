package server

import "fmt"

// pageLimit applies the limit rule every tool shares: omitted means defLimit,
// below 1 is an error and above maxLimit is capped.
func pageLimit[N int | float64](v *N, defLimit, maxLimit int) (int, error) {
	if v == nil {
		return defLimit, nil
	}

	if *v < 1 {
		return 0, fmt.Errorf("limit must be at least 1 (default %d, max %d), got %v", defLimit, maxLimit, *v)
	}

	// Capping before the conversion keeps a huge float from overflowing int.
	return int(min(*v, N(maxLimit))), nil
}
