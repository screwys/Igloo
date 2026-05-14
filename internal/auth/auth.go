package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/argon2"
)

const (
	defaultSaltLen = 16
	keyLen         = 32

	passwordAlgorithmArgon2id = "argon2id"

	defaultArgon2idMemoryKiB   = 64 * 1024
	defaultArgon2idTime        = 3
	defaultArgon2idParallelism = 2
)

// AllPlatforms is the full list of supported platforms.
var AllPlatforms = []string{"youtube", "twitter", "tiktok", "instagram"}

// PasswordRecord holds Argon2id password hash components.
type PasswordRecord struct {
	Algorithm   string `json:"algorithm,omitempty"`
	Salt        string `json:"salt"`
	Hash        string `json:"hash"`
	MemoryKiB   uint32 `json:"memory_kib,omitempty"`
	Time        uint32 `json:"time,omitempty"`
	Parallelism uint8  `json:"parallelism,omitempty"`
}

// UserRecord represents a user entry in the auth_users.json file.
type UserRecord struct {
	Password  PasswordRecord `json:"password"`
	Role      string         `json:"role"`
	Platforms []string       `json:"platforms"`
}

func (u *UserRecord) UnmarshalJSON(data []byte) error {
	// Try nested format first.
	type plain UserRecord
	var nested plain
	if err := json.Unmarshal(data, &nested); err != nil {
		return err
	}
	*u = UserRecord(nested)

	// Default role and platforms if missing.
	if u.Role == "" {
		u.Role = "admin"
	}
	if u.Platforms == nil {
		u.Platforms = AllPlatforms
	}
	return nil
}

// HashPassword generates a new Argon2id hash for the given password.
func HashPassword(password string) PasswordRecord {
	return hashPasswordArgon2id(password)
}

func hashPasswordArgon2id(password string) PasswordRecord {
	salt := randomSalt()
	dk := argon2.IDKey(
		[]byte(password),
		salt,
		defaultArgon2idTime,
		defaultArgon2idMemoryKiB,
		defaultArgon2idParallelism,
		keyLen,
	)
	return PasswordRecord{
		Algorithm:   passwordAlgorithmArgon2id,
		Salt:        base64.StdEncoding.EncodeToString(salt),
		Hash:        base64.StdEncoding.EncodeToString(dk),
		MemoryKiB:   defaultArgon2idMemoryKiB,
		Time:        defaultArgon2idTime,
		Parallelism: defaultArgon2idParallelism,
	}
}

func randomSalt() []byte {
	salt := make([]byte, defaultSaltLen)
	if _, err := rand.Read(salt); err != nil {
		panic(fmt.Sprintf("auth: rand.Read: %v", err))
	}
	return salt
}

// VerifyPassword checks whether password matches the stored record.
func VerifyPassword(password string, record PasswordRecord) bool {
	salt, err := base64.StdEncoding.DecodeString(record.Salt)
	if err != nil {
		return false
	}
	expected, err := base64.StdEncoding.DecodeString(record.Hash)
	if err != nil {
		return false
	}
	if len(expected) != keyLen {
		return false
	}
	if record.Algorithm != passwordAlgorithmArgon2id {
		return false
	}
	if record.MemoryKiB == 0 || record.Time == 0 || record.Parallelism == 0 {
		return false
	}
	dk := argon2.IDKey([]byte(password), salt, record.Time, record.MemoryKiB, record.Parallelism, uint32(len(expected)))
	return hmac.Equal(dk, expected)
}

// LoadUsers reads the auth_users.json file. Returns empty map if file is missing.
func LoadUsers(path string) (map[string]UserRecord, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]UserRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read auth users: %w", err)
	}
	var users map[string]UserRecord
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, fmt.Errorf("parse auth users: %w", err)
	}
	return users, nil
}

// SaveUsers writes users to path using an atomic temp-file + rename pattern.
// The file is created with 0600 permissions.
func SaveUsers(path string, users map[string]UserRecord) error {
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal auth users: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir auth dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".auth_users_*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write auth users: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod auth users: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename auth users: %w", err)
	}
	return nil
}

// --- In-memory user cache ---

var (
	cacheMu   sync.RWMutex
	cachePath string
	cacheData map[string]UserRecord

	// fileMu serializes load-modify-save cycles so concurrent admin requests
	// don't race on the auth_users.json file.
	fileMu sync.Mutex
)

// LockUsers acquires the file-level mutex. Callers must call UnlockUsers when done.
// Use to wrap the LoadUsers → modify → SaveUsers sequence in handlers.
func LockUsers() { fileMu.Lock() }

// UnlockUsers releases the file-level mutex.
func UnlockUsers() { fileMu.Unlock() }

// InitCache initializes the in-memory user cache from disk.
// Call once at startup with the auth_users.json path.
func InitCache(path string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cachePath = path
	cacheData, _ = LoadUsers(path)
	if cacheData == nil {
		cacheData = map[string]UserRecord{}
	}
}

// GetCachedUsers returns the current cached copy of all users.
func GetCachedUsers() map[string]UserRecord {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	// Return a shallow copy so callers can't mutate the cache.
	out := make(map[string]UserRecord, len(cacheData))
	for k, v := range cacheData {
		out[k] = v
	}
	return out
}

// InvalidateCache reloads the user cache from disk.
func InvalidateCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cachePath == "" {
		return
	}
	users, _ := LoadUsers(cachePath)
	if users == nil {
		users = map[string]UserRecord{}
	}
	cacheData = users
}
