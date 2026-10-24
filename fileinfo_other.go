//go:build !linux

package main

import "time"

func getCreationTime(path string, fallback time.Time) time.Time {
	return fallback
}
