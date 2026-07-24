package core

import "encoding/json"

func extractExtra(data []byte, known []string) (map[string]any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	knownSet := make(map[string]struct{}, len(known))
	for _, k := range known {
		knownSet[k] = struct{}{}
	}
	extra := make(map[string]any)
	for k, v := range raw {
		if _, ok := knownSet[k]; ok {
			continue
		}
		var val any
		if err := json.Unmarshal(v, &val); err != nil {
			return nil, err
		}
		extra[k] = val
	}
	if len(extra) == 0 {
		return nil, nil
	}
	return extra, nil
}

func mergeExtra(base []byte, extra map[string]any) ([]byte, error) {
	if len(extra) == 0 {
		return base, nil
	}
	var m map[string]any
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	for k, v := range extra {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return json.Marshal(m)
}
