//go:build !unix

package secrets

func ValidateSecretFile(path string) error {
	return nil
}
