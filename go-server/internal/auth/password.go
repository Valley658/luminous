package auth

import (
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

const (
	argonTime    = 3
	argonMemory  = 65536
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

var ErrInvalidHashFormat = errors.New("invalid password hash format")

func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := crand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64Salt, b64Hash), nil
}

func VerifyPassword(storedHash, password string) (ok bool, needsRehash bool) {
	if storedHash == "" {
		return false, false
	}
	if strings.HasPrefix(storedHash, "$argon2") {
		match, err := verifyArgon2(storedHash, password)
		if err != nil {
			return false, false
		}
		return match, false
	}

	match, err := verifyWerkzeugPBKDF2(storedHash, password)
	if err != nil {
		return false, false
	}
	return match, match
}

func verifyArgon2(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")

	if len(parts) != 6 {
		return false, ErrInvalidHashFormat
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	var mem uint32
	var timeCost uint32
	var threads uint8
	for _, kv := range strings.Split(parts[3], ",") {
		kvParts := strings.SplitN(kv, "=", 2)
		if len(kvParts) != 2 {
			continue
		}
		val, err := strconv.Atoi(kvParts[1])
		if err != nil {
			continue
		}
		switch kvParts[0] {
		case "m":
			mem = uint32(val)
		case "t":
			timeCost = uint32(val)
		case "p":
			threads = uint8(val)
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	wantHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	gotHash := argon2.IDKey([]byte(password), salt, timeCost, mem, threads, uint32(len(wantHash)))
	return subtle.ConstantTimeCompare(gotHash, wantHash) == 1, nil
}

func verifyWerkzeugPBKDF2(encoded, password string) (bool, error) {

	segs := strings.SplitN(encoded, "$", 3)
	if len(segs) != 3 {
		return false, ErrInvalidHashFormat
	}
	method, salt, hashHex := segs[0], segs[1], segs[2]
	methodParts := strings.Split(method, ":")
	if len(methodParts) < 2 || methodParts[0] != "pbkdf2" {
		return false, ErrInvalidHashFormat
	}
	iterations := 600000
	if len(methodParts) >= 3 {
		if n, err := strconv.Atoi(methodParts[2]); err == nil {
			iterations = n
		}
	}
	keyLen := 32
	derived := pbkdf2.Key([]byte(password), []byte(salt), iterations, keyLen, sha256.New)
	derivedHex := hex.EncodeToString(derived)
	return hmac.Equal([]byte(derivedHex), []byte(hashHex)), nil
}
