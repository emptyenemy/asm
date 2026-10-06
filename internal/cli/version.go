package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type sdkVersion [4]int

func versionParts(text string) ([]int, error) {
	parts := strings.Split(text, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return nil, errors.New("expected a version such as 51.4 or 51.4.1.1")
	}
	result := make([]int, len(parts))
	for i, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return nil, errors.New("expected a version such as 51.4 or 51.4.1.1")
		}
		n, err := strconv.ParseUint(part, 10, 31)
		if err != nil {
			return nil, errors.New("version component is too large")
		}
		result[i] = int(n)
	}
	return result, nil
}

func parseVersion(text string) (sdkVersion, error) {
	parts, err := versionParts(text)
	if err != nil {
		return sdkVersion{}, err
	}
	if len(parts) != 4 {
		return sdkVersion{}, errors.New("expected a full four-component SDK version")
	}
	return sdkVersion{parts[0], parts[1], parts[2], parts[3]}, nil
}

func (v sdkVersion) String() string {
	if v[3] < 0 {
		return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
	}
	return fmt.Sprintf("%d.%d.%d.%d", v[0], v[1], v[2], v[3])
}

func (v sdkVersion) newer(other sdkVersion) bool {
	for i := range v {
		if v[i] != other[i] {
			return v[i] > other[i]
		}
	}
	return false
}

func (v sdkVersion) matches(parts []int) bool {
	for i, part := range parts {
		if v[i] != part {
			return false
		}
	}
	return true
}

func (v sdkVersion) branch(other sdkVersion) bool {
	return v[0] == other[0] && v[1] == other[1] && v[2] == other[2]
}
