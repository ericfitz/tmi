package telemetry

// safeUint64ToInt64 safely converts uint64 to int64, capping at MaxInt64
func safeUint64ToInt64(u uint64) int64 {
	if u > 9223372036854775807 { // math.MaxInt64
		return 9223372036854775807
	}
	return int64(u)
}
