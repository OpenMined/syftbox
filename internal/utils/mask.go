package utils

func MaskSecret(s string) string {
	if len(s) <= 4 {
		return "*****"
	}
	return s[:4] + "*****"
}
