// Package redact removes literal credentials from bounded output.
package redact

// Bytes redacts complete, including overlapping, matches. At a truncated stream
// boundary it also redacts any suffix that could be the beginning of a secret.
func Bytes(data []byte, secrets []string, truncated bool) []byte {
	// Range boundaries merge overlapping secrets without modifying the bytes
	// used to find subsequent matches or creating a new unredacted prefix.
	boundaries := make([]int, len(data)+1)
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		// Prefix links keep both matching and boundary detection linear even
		// for long credentials made of repeated bytes.
		prefix := make([]int, len(secret))
		for i, matched := 1, 0; i < len(secret); i++ {
			for matched > 0 && secret[i] != secret[matched] {
				matched = prefix[matched-1]
			}
			if secret[i] == secret[matched] {
				matched++
			}
			prefix[i] = matched
		}
		matched := 0
		for i, value := range data {
			for matched > 0 && value != secret[matched] {
				matched = prefix[matched-1]
			}
			if value == secret[matched] {
				matched++
			}
			if matched == len(secret) {
				boundaries[i+1-matched]++
				boundaries[i+1]--
				matched = prefix[matched-1]
			}
		}
		if truncated && matched > 0 {
			boundaries[len(data)-matched]++
			boundaries[len(data)]--
		}
	}
	var result []byte
	active, masking := 0, false
	for i, value := range data {
		active += boundaries[i]
		if active == 0 {
			result = append(result, value)
			masking = false
		} else if !masking {
			result = append(result, "[REDACTED]"...)
			masking = true
		}
	}
	return result
}
