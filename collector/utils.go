package collector

import (
	"fmt"
	"math"
	"strconv"

	"github.com/itchyny/gojq"
)

func getLabelNames(labels []jsonLabel) []string {
	var names []string
	for _, l := range labels {
		names = append(names, l.name)
	}
	return names
}

func parseAndCompileJQExp(exp string) (*gojq.Code, error) {
	if exp == "" {
		exp = "."
	}
	query, err := gojq.Parse(exp)
	if err != nil {
		return nil, fmt.Errorf("jq query parse error %w", err)
	}

	code, err := gojq.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("jq query compile error %w", err)
	}

	return code, nil
}

func sanitizeValue(v any) (float64, error) {
	var err error
	var resultErr string

	switch v := v.(type) {

	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil

	case float64:
		return v, nil

	case bool:
		if v {
			return 1.0, nil
		}
		return 0.0, nil

	case string:
		if v, err := strconv.ParseFloat(v, 64); err == nil {
			return v, nil
		}
		resultErr = fmt.Sprintf("%s", err)

		if v, err := strconv.ParseBool(v); err == nil {
			if v {
				return 1.0, nil
			}
			return 0.0, nil
		}
		resultErr = resultErr + "; " + fmt.Sprintf("%s", err)

		if v == "<nil>" {
			return math.NaN(), nil
		}
		return 0.0, fmt.Errorf(resultErr)

	case nil:
		return math.NaN(), nil

	default:
		return 0.0, fmt.Errorf("unknown value %v type '%T'", v, v)
	}
}
