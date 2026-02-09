package management

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

func HashPassword(password string) (string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}
	salt, err := randomTokenBytes(argonSaltLen)
	if err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	encodedSalt := base64.RawStdEncoding.EncodeToString(salt)
	encodedHash := base64.RawStdEncoding.EncodeToString(hash)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, encodedSalt, encodedHash), nil
}

func VerifyPassword(password, encoded string) bool {
	parsed, err := parseArgon2(encoded)
	if err != nil {
		return false
	}
	hash := argon2.IDKey([]byte(strings.TrimSpace(password)), parsed.salt, parsed.time, parsed.memory, parsed.threads, uint32(len(parsed.hash)))
	return subtle.ConstantTimeCompare(hash, parsed.hash) == 1
}

type parsedArgon2 struct {
	memory  uint32
	time    uint32
	threads uint8
	salt    []byte
	hash    []byte
}

func parseArgon2(encoded string) (parsedArgon2, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return parsedArgon2{}, fmt.Errorf("invalid encoded hash format")
	}
	if parts[1] != "argon2id" {
		return parsedArgon2{}, fmt.Errorf("unsupported hash algorithm")
	}
	if parts[2] != "v=19" {
		return parsedArgon2{}, fmt.Errorf("unsupported argon2 version")
	}
	var out parsedArgon2
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return parsedArgon2{}, fmt.Errorf("invalid argon2 params")
	}
	for _, param := range params {
		keyValue := strings.SplitN(param, "=", 2)
		if len(keyValue) != 2 {
			return parsedArgon2{}, fmt.Errorf("invalid argon2 param")
		}
		key := keyValue[0]
		value := keyValue[1]
		switch key {
		case "m":
			parsed, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return parsedArgon2{}, err
			}
			out.memory = uint32(parsed)
		case "t":
			parsed, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return parsedArgon2{}, err
			}
			out.time = uint32(parsed)
		case "p":
			parsed, err := strconv.ParseUint(value, 10, 8)
			if err != nil {
				return parsedArgon2{}, err
			}
			out.threads = uint8(parsed)
		default:
			return parsedArgon2{}, fmt.Errorf("unknown argon2 param")
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return parsedArgon2{}, err
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return parsedArgon2{}, err
	}
	out.salt = salt
	out.hash = hash
	return out, nil
}

func randomTokenBytes(size int) ([]byte, error) {
	token := make([]byte, size)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	return token, nil
}
