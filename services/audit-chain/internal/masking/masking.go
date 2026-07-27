package masking

import (
	"encoding/json"
)

func Apply(unmasked json.RawMessage) (json.RawMessage, []string) {
	var data map[string]interface{}
	if err := json.Unmarshal(unmasked, &data); err != nil {
		return unmasked, nil
	}
	
	paths := []string{}
	
	if val, ok := data["counterparty_account_number"]; ok && val != nil {
		data["counterparty_account_number"] = "***REDACTED***"
		paths = append(paths, "/counterparty_account_number")
	}
	
	if pii, ok := data["pii"].(map[string]interface{}); ok {
		for k := range pii {
			pii[k] = "***REDACTED***"
			paths = append(paths, "/pii/"+k)
		}
		data["pii"] = pii
	}
	
	masked, _ := json.Marshal(data)
	return masked, paths
}
