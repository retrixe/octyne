package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// GenerateSalt returns a 16-character salt readable in UTF-8 format as well.
func GenerateSalt() []byte {
	saltBytes := make([]byte, 12)
	_, _ = rand.Read(saltBytes)
	salt := base64.RawStdEncoding.EncodeToString(saltBytes)
	return []byte(salt)
}

// VerifyPasswordMatchesHash checks if an Argon2id or SHA256 hash matches the provided password.
func VerifyPasswordMatchesHash(password string, hash string) bool {
	// Assume Argon2id hashing since we only support it for now
	if hash[0] == '$' {
		split := strings.Split(hash, "$")
		if split[1] != "argon2id" || split[2] != "v=19" {
			log.Println("Detected unsupported hash in users.json! Only Argon2id/SHA256 are supported.")
			return false
		}

		// Retrieve parameters m, t, and p from the hash.
		memory := 51200
		time := 1
		threads := 4
		parameters := strings.Split(split[3], ",")
		for _, parameter := range parameters {
			if strings.HasPrefix(parameter, "m=") {
				memory, _ = strconv.Atoi(parameter[2:])
			} else if strings.HasPrefix(parameter, "t=") {
				time, _ = strconv.Atoi(parameter[2:])
			} else if strings.HasPrefix(parameter, "p=") {
				threads, _ = strconv.Atoi(parameter[2:])
			}
		}
		saltValue, _ := base64.RawStdEncoding.DecodeString(split[4])
		hashValue, _ := base64.RawStdEncoding.DecodeString(split[5])
		key := argon2.IDKey(
			[]byte(password),
			saltValue,
			uint32(time),
			uint32(memory),
			uint8(threads),
			uint32(len(hashValue)))
		return bytes.Equal(hashValue, key)
	}
	// Assume SHA256 hashing
	return HashPasswordSHA256(password) == hash
}

// HashPasswordSHA256 returns the SHA256 hash of a password in hex encoding.
func HashPasswordSHA256(password string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(password)))
}

// HashPassword returns the Argon2id hash of a password in encoded form.
func HashPassword(password string) string {
	salt := GenerateSalt()
	params := "$argon2id$v=19$m=51200,t=1,p=4$" // Currently fixed parameters.
	key := argon2.IDKey([]byte(password), salt, 1, 50*1024, 4, 32)
	return params + base64.RawStdEncoding.EncodeToString(salt) +
		"$" + base64.RawStdEncoding.EncodeToString(key)
}
