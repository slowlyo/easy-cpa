package backend

import (
	"strconv"
	"strings"
)

// CompareReleaseTags 比较两个 v 前缀版本号。
func CompareReleaseTags(left, right string) int {
	leftParts := normalizeVersionParts(left)
	rightParts := normalizeVersionParts(right)
	maxLen := len(leftParts)
	if len(rightParts) > maxLen {
		maxLen = len(rightParts)
	}

	for index := 0; index < maxLen; index++ {
		leftValue := 0
		rightValue := 0
		if index < len(leftParts) {
			leftValue = leftParts[index]
		}
		if index < len(rightParts) {
			rightValue = rightParts[index]
		}
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}

// normalizeVersionParts 归一化版本号片段。
func normalizeVersionParts(tag string) []int {
	tag = strings.TrimSpace(strings.TrimPrefix(tag, "v"))
	if tag == "" {
		return nil
	}
	rawParts := strings.Split(tag, ".")
	parts := make([]int, 0, len(rawParts))
	for _, raw := range rawParts {
		value, err := strconv.Atoi(raw)
		if err != nil {
			parts = append(parts, 0)
			continue
		}
		parts = append(parts, value)
	}
	return parts
}
