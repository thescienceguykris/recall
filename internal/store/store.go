package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const LocalUserID = "00000000-0000-0000-0000-000000000000"

type contextKey string

const userIDContextKey contextKey = "store-user-id"

type User struct {
	ID              string    `json:"id"`
	Provider        string    `json:"provider"`
	ProviderSubject string    `json:"provider_subject"`
	Email           string    `json:"email,omitempty"`
	Name            string    `json:"name,omitempty"`
	PictureURL      string    `json:"picture_url,omitempty"`
	Created         time.Time `json:"created"`
	Updated         time.Time `json:"updated"`
}

type Artefact struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"owner_id,omitempty"`
	JobID     string    `json:"job_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	MediaType string    `json:"media_type"`
	Content   string    `json:"content"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated,omitempty"`
}

type Job struct {
	ID           string    `json:"id"`
	OwnerID      string    `json:"owner_id,omitempty"`
	SourceURL    string    `json:"source_url"`
	Title        string    `json:"title"`
	Tags         []string  `json:"tags"`
	Status       string    `json:"status"`
	Stage        string    `json:"stage"`
	Message      string    `json:"message"`
	Error        string    `json:"error,omitempty"`
	NoteID       string    `json:"note_id,omitempty"`
	TranscriptID string    `json:"transcript_id,omitempty"`
	Created      time.Time `json:"created"`
	Updated      time.Time `json:"updated"`
}

type DB struct {
	Path string
	mu   sync.Mutex
}

type dataFile struct {
	Users     []User     `json:"users,omitempty"`
	Jobs      []Job      `json:"jobs"`
	Artefacts []Artefact `json:"artefacts"`
}

func WithUserID(ctx context.Context, userID string) context.Context {
	if userID == "" {
		userID = LocalUserID
	}
	return context.WithValue(ctx, userIDContextKey, userID)
}

func UserIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return LocalUserID
	}
	userID, _ := ctx.Value(userIDContextKey).(string)
	if userID == "" {
		return LocalUserID
	}
	return userID
}

func New(path string) *DB {
	return &DB{Path: path}
}

func (db *DB) Init(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(db.Path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(db.Path); errors.Is(err, os.ErrNotExist) {
		return db.writeLocked(dataFile{})
	} else if err != nil {
		return err
	}
	_, err := db.readLocked()
	return err
}

func (db *DB) EnsureUser(ctx context.Context, user User) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	if user.ID == "" {
		user.ID = UserIDFromContext(ctx)
	}
	if user.ID == "" {
		user.ID = LocalUserID
	}
	now := time.Now().UTC()
	db.mu.Lock()
	defer db.mu.Unlock()
	data, err := db.readLocked()
	if err != nil {
		return User{}, err
	}
	for i := range data.Users {
		if data.Users[i].ID == user.ID || (user.Provider != "" && data.Users[i].Provider == user.Provider && data.Users[i].ProviderSubject == user.ProviderSubject) {
			if user.Provider != "" {
				data.Users[i].Provider = user.Provider
			}
			if user.ProviderSubject != "" {
				data.Users[i].ProviderSubject = user.ProviderSubject
			}
			data.Users[i].Email = user.Email
			data.Users[i].Name = user.Name
			data.Users[i].PictureURL = user.PictureURL
			data.Users[i].Updated = now
			if data.Users[i].Created.IsZero() {
				data.Users[i].Created = now
			}
			return data.Users[i], db.writeLocked(data)
		}
	}
	if user.Provider == "" {
		user.Provider = "local"
	}
	if user.ProviderSubject == "" {
		user.ProviderSubject = user.ID
	}
	user.Created = now
	user.Updated = now
	data.Users = append(data.Users, user)
	return user, db.writeLocked(data)
}

func (db *DB) UpsertJob(ctx context.Context, job Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	data, err := db.readLocked()
	if err != nil {
		return err
	}
	if job.OwnerID == "" {
		job.OwnerID = UserIDFromContext(ctx)
	}
	replaced := false
	for i := range data.Jobs {
		if data.Jobs[i].ID == job.ID {
			data.Jobs[i] = job
			replaced = true
			break
		}
	}
	if !replaced {
		data.Jobs = append(data.Jobs, job)
	}
	return db.writeLocked(data)
}

func (db *DB) AddArtefact(ctx context.Context, artefact Artefact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	data, err := db.readLocked()
	if err != nil {
		return err
	}
	if artefact.OwnerID == "" {
		artefact.OwnerID = UserIDFromContext(ctx)
	}
	data.Artefacts = append(data.Artefacts, artefact)
	return db.writeLocked(data)
}

func (db *DB) UpsertArtefact(ctx context.Context, artefact Artefact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	data, err := db.readLocked()
	if err != nil {
		return err
	}
	if artefact.OwnerID == "" {
		artefact.OwnerID = UserIDFromContext(ctx)
	}
	replaced := false
	for i := range data.Artefacts {
		if data.Artefacts[i].ID == artefact.ID {
			if artefact.Created.IsZero() {
				artefact.Created = data.Artefacts[i].Created
			}
			data.Artefacts[i] = artefact
			replaced = true
			break
		}
	}
	if !replaced {
		data.Artefacts = append(data.Artefacts, artefact)
	}
	return db.writeLocked(data)
}

func (db *DB) GetJob(ctx context.Context, id string) (Job, bool, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, false, err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	data, err := db.readLocked()
	if err != nil {
		return Job{}, false, err
	}
	ownerID := UserIDFromContext(ctx)
	for _, job := range data.Jobs {
		if job.ID == id && recordOwnerID(job.OwnerID) == ownerID {
			return job, true, nil
		}
	}
	return Job{}, false, nil
}

func (db *DB) ListJobs(ctx context.Context) ([]Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	data, err := db.readLocked()
	if err != nil {
		return nil, err
	}
	ownerID := UserIDFromContext(ctx)
	jobs := data.Jobs[:0]
	for _, job := range data.Jobs {
		if recordOwnerID(job.OwnerID) == ownerID {
			jobs = append(jobs, job)
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].Created.After(jobs[j].Created)
	})
	return jobs, nil
}

func (db *DB) GetArtefact(ctx context.Context, id string) (Artefact, bool, error) {
	if err := ctx.Err(); err != nil {
		return Artefact{}, false, err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	data, err := db.readLocked()
	if err != nil {
		return Artefact{}, false, err
	}
	ownerID := UserIDFromContext(ctx)
	for _, artefact := range data.Artefacts {
		if artefact.ID == id && recordOwnerID(artefact.OwnerID) == ownerID {
			return artefact, true, nil
		}
	}
	return Artefact{}, false, nil
}

func (db *DB) DeleteJob(ctx context.Context, id string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	data, err := db.readLocked()
	if err != nil {
		return false, err
	}

	ownerID := UserIDFromContext(ctx)
	found := false
	jobs := data.Jobs[:0]
	for _, job := range data.Jobs {
		if job.ID == id && recordOwnerID(job.OwnerID) == ownerID {
			found = true
			continue
		}
		jobs = append(jobs, job)
	}
	if !found {
		return false, nil
	}

	artefacts := data.Artefacts[:0]
	for _, artefact := range data.Artefacts {
		if artefact.JobID == id && recordOwnerID(artefact.OwnerID) == ownerID {
			continue
		}
		artefacts = append(artefacts, artefact)
	}
	data.Jobs = jobs
	data.Artefacts = artefacts
	return true, db.writeLocked(data)
}

func (db *DB) readLocked() (dataFile, error) {
	var data dataFile
	raw, err := os.ReadFile(db.Path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return dataFile{}, err
	}
	if len(raw) == 0 {
		return data, nil
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return dataFile{}, err
	}
	return data, nil
}

func (db *DB) writeLocked(data dataFile) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := db.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, db.Path)
}

func recordOwnerID(ownerID string) string {
	if ownerID == "" {
		return LocalUserID
	}
	return ownerID
}
